# Yottaflux Seed Node Deployment Guide

This guide covers building, pushing, and deploying Yottaflux seed nodes on AWS
using ECR for image storage and EC2 instances provisioned by Terraform. The
infrastructure pattern mirrors the existing `yf-infra-terraform/` setup and
adapts it for the Go-based yottaflux-go client.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Prerequisites](#prerequisites)
3. [Repository Layout](#repository-layout)
4. [Step 1 — Provision the ECR Repository](#step-1--provision-the-ecr-repository)
5. [Step 2 — Build and Push the Docker Image](#step-2--build-and-push-the-docker-image)
6. [Step 3 — Adapt the Terraform Node Module](#step-3--adapt-the-terraform-node-module)
7. [Step 4 — Deploy Seed Nodes](#step-4--deploy-seed-nodes)
8. [Step 5 — Verify the Deployment](#step-5--verify-the-deployment)
9. [Multi-Region Deployment](#multi-region-deployment)
10. [Operational Reference](#operational-reference)
11. [Environment Variables Reference](#environment-variables-reference)
12. [Troubleshooting](#troubleshooting)

---

## Architecture Overview

```
                         Route 53 (latency-based)
                        seed.yottaflux.network
                       /                        \
              us-east-1                          eu-west-1
             /         \                        /         \
         AZ-a          AZ-b                 AZ-a          AZ-b
      [EC2+EIP]     [EC2+EIP]           [EC2+EIP]     [EC2+EIP]
      Docker:        Docker:             Docker:        Docker:
      yottaflux     yottaflux           yottaflux     yottaflux
      seed node     seed node           seed node     seed node
           \           /                     \           /
            \         /                       \         /
         ECR (us-east-1)                   ECR (eu-west-1)
```

Each EC2 instance:
- Pulls the `yottaflux-seed` Docker image from ECR on boot
- Auto-initializes the genesis block on first start
- Runs as a non-mining, high-peer-count relay node
- Gets a static Elastic IP for stable DNS
- Checks for image updates every 5 minutes via cron

DNS is managed by Route 53 with latency-based routing, so clients connecting to
`seed.yottaflux.network` are directed to the closest region automatically.

---

## Prerequisites

- **AWS CLI** configured with credentials that can manage ECR, EC2, VPC, IAM,
  and Route 53
- **Terraform** >= 1.3.0
- **Docker** for building images locally
- **SSH key pair** — you will need the public key material for Terraform and the
  private key for SSH access
- A **Route 53 hosted zone** for your domain

---

## Repository Layout

```
yottaflux-go/
  Dockerfile                  # Minimal image (just the binary)
  Dockerfile.alltools         # All tools image
  genesis_yottaflux.json      # Genesis block definition
  contrib/
    Dockerfile                # Seed node image (genesis baked in, entrypoint)
    docker-entrypoint.sh      # Smart entrypoint with env-var configuration
    DEPLOYMENT.md             # This guide

yf-infra-terraform/           # Infrastructure-as-code (separate repo)
  ecr/
    main.tf                   # ECR repository + lifecycle policy
    vars.tf
    outputs.tf
    versions.tf
  node/
    main.tf                   # VPC, SG, IAM, EC2, EIP, Route 53
    vars.tf
    versions.tf
```

---

## Step 1 — Provision the ECR Repository

The ECR module creates a private Docker registry in your AWS account. This only
needs to be done once per region.

### 1.1 Create a tfvars file

```hcl
# ecr/terraform.tfvars
repository_name    = "yottaflux-seed"
environment        = "prod"
aws_region         = "us-east-1"
scan_on_push       = true
max_image_count    = 30
```

### 1.2 Apply

```bash
cd yf-infra-terraform/ecr
terraform init
terraform plan -var-file=terraform.tfvars
terraform apply -var-file=terraform.tfvars
```

### 1.3 Note the output

```
repository_url = "123456789012.dkr.ecr.us-east-1.amazonaws.com/yottaflux-seed"
```

Save this — you will need it for the image push and node module.

---

## Step 2 — Build and Push the Docker Image

### 2.1 Build the seed node image

From the root of the `yottaflux-go` repository:

```bash
docker build -f contrib/Dockerfile -t yottaflux-seed:latest .
```

This performs a multi-stage build:
1. Compiles the `yottaflux` binary statically in a Go 1.19 Alpine container
2. Copies the binary and `genesis_yottaflux.json` into a minimal Alpine runtime
3. Sets up a non-root `yottaflux` user, data volume, and the entrypoint

### 2.2 Tag for ECR

Replace `123456789012` and `us-east-1` with your account ID and region:

```bash
ECR_URL="123456789012.dkr.ecr.us-east-1.amazonaws.com/yottaflux-seed"

docker tag yottaflux-seed:latest ${ECR_URL}:prod
docker tag yottaflux-seed:latest ${ECR_URL}:latest
```

Use the environment name as the tag (`prod`, `stage`, `dev`). The Terraform
user data script pulls `${ECR_URL}:${environment}`.

### 2.3 Authenticate and push

```bash
aws ecr get-login-password --region us-east-1 | \
  docker login --username AWS --password-stdin ${ECR_URL}

docker push ${ECR_URL}:prod
docker push ${ECR_URL}:latest
```

### 2.4 Verify

```bash
aws ecr describe-images \
  --repository-name yottaflux-seed \
  --region us-east-1 \
  --query 'imageDetails[*].{Tags:imageTags,Pushed:imagePushedAt,Size:imageSizeInBytes}' \
  --output table
```

---

## Step 3 — Adapt the Terraform Node Module

The existing `node/` module needs several changes to work with the Go-based
yottaflux-go container instead of the Bitcoin-fork yottafluxd.

### 3.1 Key differences from the original setup

| Aspect | Old (yottafluxd) | New (yottaflux-go) |
|--------|-------------------|---------------------|
| Binary | `yottafluxd` (C++) | `yottaflux` (Go) |
| P2P port | 8559 | 30403 |
| RPC port | 8558 | 8645 |
| Config | `NETWORK` env var | Multiple env vars (see reference) |
| Genesis | Built into binary | Init'd by entrypoint from baked-in JSON |
| Protocol | Bitcoin-derived | Ethereum/devp2p |
| Discovery | DNS seeds | Kademlia DHT + bootnodes |

### 3.2 Update the tfvars

```hcl
# node/terraform.tfvars
key_name      = "yottaflux-seed-key"
public_key    = "ssh-ed25519 AAAA... you@host"
name          = "yfx-seed"
instance_type = "t3.medium"
cidr          = "10.0.0.0/16"
node_port     = 30403
aws_region    = "us-east-1"
azs           = ["a", "b"]
domain_name   = "seed.yottaflux.network"
ecr_repo_name = "yottaflux-seed"
environment   = "prod"
```

**Instance sizing guidance:**
- `t3.medium` (2 vCPU, 4 GB) — sufficient for a seed node that does not mine;
  peer relay is not memory-intensive
- `t3.large` (2 vCPU, 8 GB) — recommended if you expect heavy sync traffic or
  want headroom for snap sync serving
- `m6i.large` or larger — if the node will also serve RPC traffic

### 3.3 Update the security group

The security group needs to allow P2P on both TCP and UDP (devp2p uses UDP for
node discovery). If you also want RPC access, open 8645.

```hcl
ingress {
  description = "YFX P2P TCP"
  protocol    = "tcp"
  from_port   = 30403
  to_port     = 30403
  cidr_blocks = ["0.0.0.0/0"]
}

ingress {
  description = "YFX P2P UDP (discovery)"
  protocol    = "udp"
  from_port   = 30403
  to_port     = 30403
  cidr_blocks = ["0.0.0.0/0"]
}

# Optional: HTTP RPC (restrict to your IP or VPN in production)
ingress {
  description = "YFX HTTP RPC"
  protocol    = "tcp"
  from_port   = 8645
  to_port     = 8645
  cidr_blocks = ["10.0.0.0/8"]   # internal only — adjust as needed
}
```

### 3.4 Update the user data script

The user data script in `node/main.tf` (the `local.user_data` block) needs to
be adapted for the new container's environment variables and port mappings.

Replace the `docker run` command and the update script's `docker run` with:

```bash
# --- in user_data ---

IMAGE="${local.ecr_repo_url}:${var.environment}"

docker run -d \
  --name yottaflux-seed \
  --restart always \
  -v yfx-data:/var/lib/yottaflux \
  -p 30403:30403/tcp \
  -p 30403:30403/udp \
  -p 8645:8645/tcp \
  -e ENABLE_RPC=true \
  -e MAX_PEERS=100 \
  -e NAT=extip:$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4) \
  $IMAGE
```

Key points:

- **Named volume** (`yfx-data`) — persists chain data across container restarts
  and image updates. Without this, every update would re-sync from genesis.
- **UDP port** — required for devp2p node discovery. The original setup only
  mapped TCP because the Bitcoin protocol doesn't use UDP discovery.
- **`NAT=extip:...`** — queries the EC2 instance metadata service to get the
  public IP (the Elastic IP). This tells the node to advertise its real public
  IP to peers rather than the container's internal Docker IP.
- **`ENABLE_RPC=true`** — enables the HTTP JSON-RPC endpoint so the healthcheck
  and monitoring can query the node. Bind it to 0.0.0.0 inside the container
  (the entrypoint does this automatically) and restrict access via the security
  group.

### 3.5 Update the auto-update script

The update script (`/usr/local/bin/update-yotta-node.sh`) in the user data
needs the same `docker run` flags. The critical addition is `-v yfx-data:...`
so the new container reattaches the same chain data volume:

```bash
cat > /usr/local/bin/update-yottaflux-seed.sh << 'SCRIPT'
#!/bin/bash
IMAGE="${local.ecr_repo_url}:${var.environment}"
REGION="${var.aws_region}"
LOG="/var/log/yottaflux-update.log"

# Re-authenticate with ECR
aws ecr get-login-password --region $REGION | \
  docker login --username AWS --password-stdin ${local.ecr_repo_url} >> $LOG 2>&1

# Get image ID before pull
OLD_ID=$(docker images --no-trunc --format '{{.ID}}' "$IMAGE" 2>/dev/null)

# Pull latest
docker pull "$IMAGE" >> $LOG 2>&1

# Get image ID after pull
NEW_ID=$(docker images --no-trunc --format '{{.ID}}' "$IMAGE" 2>/dev/null)

if [ "$OLD_ID" != "$NEW_ID" ] && [ -n "$NEW_ID" ]; then
  echo "$(date): New image detected ($OLD_ID -> $NEW_ID), restarting" >> $LOG
  docker stop yottaflux-seed >> $LOG 2>&1
  docker rm yottaflux-seed >> $LOG 2>&1
  PUBLIC_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
  docker run -d \
    --name yottaflux-seed \
    --restart always \
    -v yfx-data:/var/lib/yottaflux \
    -p 30403:30403/tcp \
    -p 30403:30403/udp \
    -p 8645:8645/tcp \
    -e ENABLE_RPC=true \
    -e MAX_PEERS=100 \
    -e NAT=extip:$PUBLIC_IP \
    "$IMAGE" >> $LOG 2>&1
else
  echo "$(date): No update needed" >> $LOG
fi
SCRIPT
chmod +x /usr/local/bin/update-yottaflux-seed.sh

echo "*/5 * * * * root /usr/local/bin/update-yottaflux-seed.sh" \
  > /etc/cron.d/yottaflux-seed-update
chmod 644 /etc/cron.d/yottaflux-seed-update
```

### 3.6 Full adapted user data block

Here is the complete `local.user_data` replacement for `node/main.tf`:

```hcl
locals {
  ecr_repo_url = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.aws_region}.amazonaws.com/${var.ecr_repo_name}"

  user_data = <<-EOT
    #!/bin/bash
    set -ex

    echo "=== Yottaflux seed node bootstrap ==="
    echo "Environment: ${var.environment}"
    echo "Region:      ${var.aws_region}"
    echo "ECR:         ${local.ecr_repo_url}"

    # System updates
    yum update -y

    # Install Docker and AWS CLI
    amazon-linux-extras install docker -y
    yum install -y awscli
    systemctl enable docker
    systemctl start docker

    # ECR login
    aws ecr get-login-password --region ${var.aws_region} | \
      docker login --username AWS --password-stdin ${local.ecr_repo_url}

    # Get the instance's public IP (Elastic IP) for NAT advertisement
    PUBLIC_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)

    # Launch the seed node container
    IMAGE="${local.ecr_repo_url}:${var.environment}"

    docker run -d \
      --name yottaflux-seed \
      --restart always \
      -v yfx-data:/var/lib/yottaflux \
      -p ${var.node_port}:${var.node_port}/tcp \
      -p ${var.node_port}:${var.node_port}/udp \
      -p 8645:8645/tcp \
      -e ENABLE_RPC=true \
      -e P2P_PORT=${var.node_port} \
      -e MAX_PEERS=100 \
      -e NAT=extip:$PUBLIC_IP \
      $IMAGE

    # --- Auto-update script ---
    cat > /usr/local/bin/update-yottaflux-seed.sh << 'SCRIPT'
    #!/bin/bash
    IMAGE="${local.ecr_repo_url}:${var.environment}"
    REGION="${var.aws_region}"
    LOG="/var/log/yottaflux-update.log"

    aws ecr get-login-password --region $REGION | \
      docker login --username AWS --password-stdin ${local.ecr_repo_url} >> $LOG 2>&1

    OLD_ID=$(docker images --no-trunc --format '{{.ID}}' "$IMAGE" 2>/dev/null)
    docker pull "$IMAGE" >> $LOG 2>&1
    NEW_ID=$(docker images --no-trunc --format '{{.ID}}' "$IMAGE" 2>/dev/null)

    if [ "$OLD_ID" != "$NEW_ID" ] && [ -n "$NEW_ID" ]; then
      echo "$(date): Updating ($OLD_ID -> $NEW_ID)" >> $LOG
      docker stop yottaflux-seed >> $LOG 2>&1
      docker rm yottaflux-seed >> $LOG 2>&1
      PUBLIC_IP=$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
      docker run -d \
        --name yottaflux-seed \
        --restart always \
        -v yfx-data:/var/lib/yottaflux \
        -p ${var.node_port}:${var.node_port}/tcp \
        -p ${var.node_port}:${var.node_port}/udp \
        -p 8645:8645/tcp \
        -e ENABLE_RPC=true \
        -e P2P_PORT=${var.node_port} \
        -e MAX_PEERS=100 \
        -e NAT=extip:$PUBLIC_IP \
        "$IMAGE" >> $LOG 2>&1
    else
      echo "$(date): No update needed" >> $LOG
    fi
    SCRIPT
    chmod +x /usr/local/bin/update-yottaflux-seed.sh

    echo "*/5 * * * * root /usr/local/bin/update-yottaflux-seed.sh" \
      > /etc/cron.d/yottaflux-seed-update
    chmod 644 /etc/cron.d/yottaflux-seed-update

    echo "=== Bootstrap complete ==="
  EOT
}
```

---

## Step 4 — Deploy Seed Nodes

### 4.1 Apply the node module

```bash
cd yf-infra-terraform/node
terraform init
terraform plan -var-file=terraform.tfvars
terraform apply -var-file=terraform.tfvars
```

This creates (per region):
- 1 VPC with public subnets in each AZ
- 1 security group allowing SSH + P2P + optional RPC
- 1 IAM role with ECR pull permissions
- 1 EC2 instance per AZ (e.g., 2 instances for `azs = ["a", "b"]`)
- 1 Elastic IP per instance
- Route 53 records with latency-based routing

### 4.2 What happens on instance boot

1. Amazon Linux 2 starts, runs the user data script
2. Docker is installed and started
3. The instance authenticates with ECR using its IAM role
4. The `yottaflux-seed` image is pulled
5. The container starts:
   - Entrypoint detects first run (no chaindata), initializes genesis
   - Node starts in relay mode: no mining, 100 max peers, NAT set to Elastic IP
   - P2P listens on 30403 (TCP+UDP), RPC on 8645
6. Cron job begins polling ECR every 5 minutes for image updates

---

## Step 5 — Verify the Deployment

### 5.1 SSH into an instance

```bash
ssh -i ~/.ssh/yottaflux-seed-key ec2-user@<elastic-ip>
```

### 5.2 Check the container

```bash
# Container running?
docker ps

# Logs (live)
docker logs -f yottaflux-seed

# Expected output includes:
#   "Chain ID:  7847 (yottaflux)"
#   "Consensus: ProgPow (proof-of-work)"
#   "Network is pure proof-of-work (ProgPow), no merge transition."
#   "IPC endpoint opened  url=/var/lib/yottaflux/yottaflux.ipc"
#   "Started P2P networking"
```

### 5.3 Query via RPC

```bash
# From the instance (or remotely if SG allows):
curl -s -X POST http://127.0.0.1:8645 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"net_peerCount","params":[],"id":1}'
# {"jsonrpc":"2.0","id":1,"result":"0x5"}  (5 peers)

curl -s -X POST http://127.0.0.1:8645 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}'
# {"jsonrpc":"2.0","id":1,"result":"0x1a4"}  (block 420)

curl -s -X POST http://127.0.0.1:8645 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}' | python3 -m json.tool
# Shows enode URL, protocols, network ID, etc.
```

### 5.4 Get the enode URL

The enode URL is what other nodes need to connect to this seed node. Query it
via RPC or the logs:

```bash
# Via RPC
curl -s -X POST http://127.0.0.1:8645 \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"admin_nodeInfo","params":[],"id":1}' \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['result']['enode'])"

# Output:
# enode://abc123...@203.0.113.10:30403
```

Save the enode URLs from all seed nodes. These become the `--bootnodes` list
for regular nodes joining the network.

### 5.5 DNS verification

```bash
# Latency-routed record (returns nearest region's IPs)
dig seed.yottaflux.network A +short

# Region-specific
dig us-east-1-seed.yottaflux.network A +short

# AZ-specific
dig us-east-1a-seed.yottaflux.network A +short
```

---

## Multi-Region Deployment

To deploy across multiple regions, run the node module once per region with
different variable files. Each region gets its own VPC, instances, and EIPs,
but they all share the same Route 53 latency-based record.

### Example: 3-region deployment

```bash
# Region 1
cd yf-infra-terraform/node
terraform workspace new us-east-1 || terraform workspace select us-east-1
terraform apply -var-file=us-east-1.tfvars

# Region 2
terraform workspace new eu-west-1 || terraform workspace select eu-west-1
terraform apply -var-file=eu-west-1.tfvars

# Region 3
terraform workspace new ap-southeast-1 || terraform workspace select ap-southeast-1
terraform apply -var-file=ap-southeast-1.tfvars
```

Each region needs its own ECR repository (ECR is regional). Push the same image
to each:

```bash
for REGION in us-east-1 eu-west-1 ap-southeast-1; do
  ECR_URL="123456789012.dkr.ecr.${REGION}.amazonaws.com/yottaflux-seed"
  aws ecr get-login-password --region $REGION | \
    docker login --username AWS --password-stdin $ECR_URL
  docker tag yottaflux-seed:latest ${ECR_URL}:prod
  docker push ${ECR_URL}:prod
done
```

Route 53 latency-based routing automatically directs clients to the nearest
region. The `set_identifier` in the Terraform record distinguishes each
region's contribution to the same DNS name.

### Connecting seed nodes across regions

Once all seeds are running, collect their enode URLs and update each node to
know about the others using the `BOOTNODES` environment variable or static
node files:

```bash
# On each seed node, restart the container with BOOTNODES pointing to the
# other seeds:
docker stop yottaflux-seed && docker rm yottaflux-seed

docker run -d \
  --name yottaflux-seed \
  --restart always \
  -v yfx-data:/var/lib/yottaflux \
  -p 30403:30403/tcp \
  -p 30403:30403/udp \
  -p 8645:8645/tcp \
  -e ENABLE_RPC=true \
  -e MAX_PEERS=100 \
  -e NAT=extip:$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4) \
  -e BOOTNODES="enode://aaa@1.2.3.4:30403,enode://bbb@5.6.7.8:30403" \
  ${ECR_URL}:prod
```

Alternatively, mount a `static-nodes.json` file into the container:

```bash
# Create the file
cat > /opt/yottaflux/static-nodes.json << 'EOF'
[
  "enode://aaa...@1.2.3.4:30403",
  "enode://bbb...@5.6.7.8:30403",
  "enode://ccc...@9.10.11.12:30403"
]
EOF

# Mount it into the container's data dir
docker run -d \
  --name yottaflux-seed \
  --restart always \
  -v yfx-data:/var/lib/yottaflux \
  -v /opt/yottaflux/static-nodes.json:/var/lib/yottaflux/yottaflux/static-nodes.json:ro \
  -p 30403:30403/tcp \
  -p 30403:30403/udp \
  ...
```

---

## Operational Reference

### Updating the node software

1. Build a new image from the latest source:
   ```bash
   cd yottaflux-go
   docker build -f contrib/Dockerfile -t yottaflux-seed:latest .
   ```

2. Tag and push to ECR:
   ```bash
   docker tag yottaflux-seed:latest ${ECR_URL}:prod
   docker push ${ECR_URL}:prod
   ```

3. Wait up to 5 minutes for the cron job to detect the new image and restart
   the container. Or force an immediate update:
   ```bash
   ssh ec2-user@<ip> "sudo /usr/local/bin/update-yottaflux-seed.sh"
   ```

Chain data is preserved across updates via the Docker named volume.

### Viewing logs

```bash
# Live container logs
docker logs -f yottaflux-seed

# Auto-update log
cat /var/log/yottaflux-update.log
tail -f /var/log/yottaflux-update.log

# User data bootstrap log (Amazon Linux 2)
cat /var/log/cloud-init-output.log
```

### Inspecting chain data

```bash
# Volume location on host
docker volume inspect yfx-data

# Exec into the container
docker exec -it yottaflux-seed sh

# Inside the container:
ls /var/lib/yottaflux/yottaflux/
# chaindata/  lightchaindata/  nodekey  nodes/  LOCK  ...
```

### Restarting the node

```bash
# Restart (keeps same container)
docker restart yottaflux-seed

# Full stop + start (same image, same volume)
docker stop yottaflux-seed
docker start yottaflux-seed
```

### Wiping chain data and re-syncing

```bash
docker stop yottaflux-seed
docker rm yottaflux-seed
docker volume rm yfx-data
# Re-run the docker run command — entrypoint will re-init genesis
```

### Backup the node key

The node key determines the node's identity (enode URL). Back it up if you want
to preserve the identity across data wipes:

```bash
docker cp yottaflux-seed:/var/lib/yottaflux/yottaflux/nodekey ./nodekey.backup
```

Restore it by mounting:

```bash
docker run -d \
  -v /path/to/nodekey.backup:/var/lib/yottaflux/yottaflux/nodekey:ro \
  ...
```

---

## Environment Variables Reference

All variables are optional. Defaults produce a working seed node.

| Variable | Default | Description |
|----------|---------|-------------|
| `NETWORK_ID` | `7847` | Chain network ID |
| `P2P_PORT` | `30403` | P2P listen port (TCP + UDP) |
| `MAX_PEERS` | `100` | Maximum connected peers |
| `NAT` | `any` | NAT traversal mode. Use `extip:<ip>` for known public IPs |
| `SYNC_MODE` | `snap` | Sync strategy: `snap`, `full`, or `light` |
| `ENABLE_RPC` | `false` | Enable HTTP JSON-RPC server |
| `HTTP_PORT` | `8645` | HTTP RPC listen port |
| `HTTP_API` | `eth,net,web3` | Enabled RPC API namespaces |
| `HTTP_VHOSTS` | `*` | Allowed virtual hostnames |
| `HTTP_CORS` | `*` | CORS allowed origins |
| `ENABLE_WS` | `false` | Enable WebSocket server |
| `WS_PORT` | `8646` | WebSocket listen port |
| `WS_API` | `eth,net,web3` | Enabled WS API namespaces |
| `WS_ORIGINS` | `*` | Allowed WebSocket origins |
| `NODISCOVER` | `false` | Disable peer discovery (not recommended for seeds) |
| `BOOTNODES` | *(empty)* | Comma-separated enode URLs |
| `EXTRA_FLAGS` | *(empty)* | Additional CLI flags passed verbatim |

---

## Troubleshooting

### Container exits immediately

```bash
docker logs yottaflux-seed
```

Common causes:
- **"Fatal: Failed to create the protocol stack: datadir already used"** — a
  previous container is still holding the lock. Run `docker rm -f yottaflux-seed`
  first.
- **Genesis init failure** — the genesis JSON is missing or malformed. Check
  that `genesis_yottaflux.json` was correctly copied into the image at
  `/etc/yottaflux/genesis.json`.

### Node has 0 peers

- Verify the security group allows inbound TCP+UDP on port 30403
- Check that `NAT` is set correctly. On EC2, use
  `NAT=extip:<elastic-ip>`. Using `NAT=any` works if UPnP is available but
  EC2 does not support UPnP — the node will still work but may not be
  discoverable.
- The network needs at least one other running node. If this is the first seed,
  it will have 0 peers until others connect.

### Chain is stuck / not syncing

- Ensure the node is connected to peers (`net_peerCount`)
- Check that connected peers are on the same network ID (7847) and genesis hash
- A seed node does not mine — block number only advances when miners on the
  network produce blocks

### ECR pull fails on boot

- Verify the IAM instance profile has `AmazonEC2ContainerRegistryPullOnly`
- Check that the ECR repository exists in the same region as the instance
- Review `/var/log/cloud-init-output.log` for the ECR login output

### Auto-update not working

- Check the cron job: `cat /etc/cron.d/yottaflux-seed-update`
- Check the update log: `tail -20 /var/log/yottaflux-update.log`
- Manually run: `sudo /usr/local/bin/update-yottaflux-seed.sh`

### NAT and discovery behind Docker

Docker adds a network layer. The node sees its container IP (e.g., 172.17.0.2)
but needs to advertise the host's public IP. Always set `NAT=extip:<public-ip>`
on cloud instances. The entrypoint and user data examples query the EC2 metadata
service to resolve this automatically:

```bash
NAT=extip:$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)
```
