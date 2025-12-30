# VyOS Containerlab Tests

This suite validates the VyOS gateway configuration using a Containerlab
topology and pytest. The test environment remaps production interfaces
(eth0 WAN, eth1 trunk) to test interfaces (eth2 WAN, eth3 trunk) because
Containerlab reserves eth0 for management.

## Prerequisites

- Docker (or compatible runtime)
- Containerlab
- `sqfs2tar` from `squashfs-tools-ng`
- Python with the dependencies in `requirements.txt`
- `just` (optional, for local workflow helpers)

## Local Workflow

From `infrastructure/network/vyos`:

1) Build the VyOS container image (requires a `filesystem.squashfs` artifact)

```
just image SQUASHFS=build/live/filesystem.squashfs
```

2) Generate a test SSH key + config.boot

```
just config
```

3) Deploy the Containerlab topology

```
just deploy
```

4) Run tests

```
just pytest
```

5) Destroy the lab

```
just destroy
```

You can also run the full sequence with:

```
just test
```

## Environment Overrides

- `VYOS_HOST` (default: `clab-vyos-gateway-test-gateway`)
- `VYOS_CONTAINER` (default: `VYOS_HOST`, used for `docker exec` config checks)
- `VYOS_USER` (default: `vyos`)
- `VYOS_PASS` (default: `vyos`)
- `VYOS_SSH_KEY` (path to private key for SSH auth)
