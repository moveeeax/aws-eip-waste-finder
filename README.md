# aws-eip-waste-finder

[![ci](https://github.com/moveeeax/aws-eip-waste-finder/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/aws-eip-waste-finder/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Find allocated Elastic IPs that are **billing while doing nothing**, and total the monthly dollar waste.

Since 2024-02-01 AWS bills **every** public IPv4 address at $0.005/hr (~$3.65/mo), Elastic IPs included — whether the EIP is attached to a running instance or sitting idle ([VPC pricing](https://aws.amazon.com/vpc/pricing/), [announcement](https://aws.amazon.com/blogs/aws/new-aws-public-ipv4-address-charge-public-ip-insights/)). An attached EIP at least earns its keep. Once its instance stops or is terminated, or it was never attached, you keep paying the same rate for a public IP nobody is using. They pile up from torn-down stacks, failed NAT setups, and forgotten test boxes — each one a rounding error until there are dozens across regions.

## How it works

1. `DescribeAddresses` lists every allocated Elastic IP in the region.
2. Each address is classified:
   - **`UNATTACHED`** — no `AssociationId` at all → billing, pure waste.
   - **`ATTACHED_TO_STOPPED`** — associated with an instance that is not `running` → billing, waste.
   - **`ACTIVE`** — attached to a running instance (or a non-instance target such as a NAT gateway / ENI) → still billing, but in use, so not waste.
3. For instance-backed EIPs it batches a `DescribeInstances` to check the real power state.
4. Every EIP is priced at the public IPv4 rate (`$0.005/hr × 730 hr`). The report shows both the total monthly charge and the waste (the charge of idle EIPs only).
5. It prints a table (or `--json`) and **exits `1` when idle EIPs are found**, so it doubles as a CI gate.

Tests run entirely offline against an injected client interface — no live AWS calls.

Prices are list prices. Free Tier hours and BYOIP addresses (which AWS does not charge) are not taken into account.

## Install

Requires Go 1.26 or newer.

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
STATE                PUBLIC_IP     ALLOCATION_ID               INSTANCE             COST_$/MO  WASTE_$/MO
UNATTACHED           52.29.0.14    eipalloc-0a1b2c3d4e5f60011  -                    3.65       3.65
ATTACHED_TO_STOPPED  3.120.44.7    eipalloc-0a1b2c3d4e5f60022  i-0abcd1234ef567890  3.65       3.65
ACTIVE               18.196.7.201  eipalloc-0a1b2c3d4e5f60033  i-0999aaaa8888bbbb7  3.65       0.00

3 allocated, 2 idle — estimated waste $7.30/mo of $10.95/mo billed (region eu-central-1 @ $0.0050/hr per public IPv4)
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

---

**Archived on 2026-11-05.** merge into aws-orphan-finder: it already scans unattached EIPs multi-region with pricing; this repo's only extra is the ATTACHED_TO_STOPPED state. Also its core premise is outdated: since 2024-02-01 every public IPv4 bills $0.005/h, attached or not, so "ACTIVE = $0" is wrong. The scope moves to [aws-orphan-finder](https://github.com/moveeeax/aws-orphan-finder/issues/6).
