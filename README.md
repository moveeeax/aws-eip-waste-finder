# aws-eip-waste-finder

[![ci](https://github.com/moveeeax/aws-eip-waste-finder/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/aws-eip-waste-finder/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Find allocated Elastic IPs that are **billing while doing nothing**, and total the monthly dollar waste.

An Elastic IP is free while it's attached to a *running* instance. The moment that instance stops or is terminated, the EIP keeps billing at ~$0.005/hr (~$3.65/mo) for a public IP nobody is using. They pile up from torn-down stacks, failed NAT setups, and forgotten test boxes — each one a rounding error until there are dozens across regions.

## How it works

1. `DescribeAddresses` lists every allocated Elastic IP in the region.
2. Each address is classified:
   - **`UNATTACHED`** — no `AssociationId` at all → billing, pure waste.
   - **`ATTACHED_TO_STOPPED`** — associated with an instance that is not `running` → billing.
   - **`ACTIVE`** — attached to a running instance (or a non-instance target such as a NAT gateway / ENI) → not waste.
3. For instance-backed EIPs it batches a `DescribeInstances` to check the real power state.
4. Idle EIPs are priced from a static per-region table (`$0.005/hr × 730 hr`) and summed.
5. It prints a table (or `--json`) and **exits `1` when idle EIPs are found**, so it doubles as a CI gate.

Tests run entirely offline against an injected client interface — no live AWS calls.

## Install

```sh
go install github.com/moveeeax/aws-eip-waste-finder/cmd/aws-eip-waste-finder@latest
```

Or build from source:

```sh
git clone https://github.com/moveeeax/aws-eip-waste-finder.git
cd aws-eip-waste-finder
go build ./cmd/aws-eip-waste-finder
```

## Usage

```
usage: aws-eip-waste-finder [--region R] [--json] [--min-waste N]

  --region string      AWS region to scan (default: from env/profile)
  --json               emit JSON instead of a table
  --min-waste float    only fail (exit 1) when total monthly waste exceeds this USD amount
```

Credentials come from the standard AWS chain (env vars, shared config, SSO, instance role). The `ec2:DescribeAddresses` and `ec2:DescribeInstances` read-only permissions are enough.

### Table output

```sh
$ aws-eip-waste-finder --region eu-central-1
STATE                PUBLIC_IP     ALLOCATION_ID                  INSTANCE             MONTHLY_$
UNATTACHED           52.29.0.14    eipalloc-0a1b2c3d4e5f60011     -                    3.65
ATTACHED_TO_STOPPED  3.120.44.7    eipalloc-0a1b2c3d4e5f60022     i-0abcd1234ef567890  3.65
ACTIVE               18.196.7.201  eipalloc-0a1b2c3d4e5f60033     i-0999aaaa8888bbbb7  0.00

3 allocated, 2 idle — estimated waste $7.30/mo (region eu-central-1 @ $0.0050/hr)
```

### JSON output

```sh
$ aws-eip-waste-finder --region eu-central-1 --json
```

See [`examples/sample-report.json`](examples/sample-report.json) for the shape.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | No idle EIPs found (or total waste ≤ `--min-waste`) |
| `1`  | Idle EIPs found — CI gate |
| `2`  | Runtime error (credentials, region, AWS API) |

## License

MIT — see [LICENSE](LICENSE).
