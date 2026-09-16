// Command aws-eip-waste-finder scans a region for allocated Elastic IPs that
// are billing while doing nothing, and totals the monthly dollar waste.
//
// Exit codes:
//
//	0  no idle EIPs found
//	1  idle EIPs found (CI gate)
//	2  runtime error (AWS/credentials/flags)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/moveeeax/aws-eip-waste-finder/internal/finder"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("aws-eip-waste-finder", flag.ContinueOnError)
	fs.SetOutput(stderr)
	region := fs.String("region", "", "AWS region to scan (default: from env/profile)")
	asJSON := fs.Bool("json", false, "emit JSON instead of a table")
	minWaste := fs.Float64("min-waste", 0, "only fail (exit 1) when total monthly waste exceeds this USD amount")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: aws-eip-waste-finder [--region R] [--json] [--min-waste N]\n\n")
		fmt.Fprintf(stderr, "Scans allocated Elastic IPs and reports idle (billing) ones.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	var opts []func(*config.LoadOptions) error
	if *region != "" {
		opts = append(opts, config.WithRegion(*region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "error: load AWS config: %v\n", err)
		return 2
	}
	if cfg.Region == "" {
		fmt.Fprintf(stderr, "error: no region set (use --region or AWS_REGION)\n")
		return 2
	}

	client := ec2.NewFromConfig(cfg)
	rep, err := finder.Scan(ctx, client, cfg.Region)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	if *asJSON {
		if err := rep.WriteJSON(stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	} else {
		if err := rep.WriteTable(stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
	}

	if rep.IdleCount > 0 && rep.TotalMonthly > *minWaste {
		return 1
	}
	return 0
}
