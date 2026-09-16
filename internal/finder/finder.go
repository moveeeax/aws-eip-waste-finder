// Package finder classifies allocated Elastic IPs and estimates the monthly
// dollar waste of the ones that are billing while doing nothing.
package finder

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// State classifies an allocated Elastic IP.
type State string

const (
	// StateActive means the EIP is associated with a running instance — not waste.
	StateActive State = "ACTIVE"
	// StateUnattached means the EIP has no association at all — pure waste.
	StateUnattached State = "UNATTACHED"
	// StateAttachedToStopped means the EIP is associated with an instance that
	// is not in the "running" state — billing for a stopped box.
	StateAttachedToStopped State = "ATTACHED_TO_STOPPED"
)

// idle reports whether a state bills while providing no value.
func (s State) idle() bool {
	return s == StateUnattached || s == StateAttachedToStopped
}

// Default EIP idle price: AWS bills an unassociated/idle Elastic IP at
// $0.005 per hour in most commercial regions.
const defaultHourlyUSD = 0.005

// hoursPerMonth is the AWS billing convention (730 hours/month).
const hoursPerMonth = 730.0

// regionHourly overrides the default idle price for regions that differ.
// Values are USD/hour for an idle (unassociated) Elastic IP.
var regionHourly = map[string]float64{
	// GovCloud and a few others historically priced idle EIPs slightly higher.
	"us-gov-west-1": 0.005,
	"us-gov-east-1": 0.005,
}

// HourlyPrice returns the idle-EIP hourly USD price for a region.
func HourlyPrice(region string) float64 {
	if p, ok := regionHourly[region]; ok {
		return p
	}
	return defaultHourlyUSD
}

// MonthlyWaste returns the estimated monthly USD waste for one idle EIP in a region.
func MonthlyWaste(region string) float64 {
	return HourlyPrice(region) * hoursPerMonth
}

// Address is the finder's normalized view of one allocated Elastic IP.
type Address struct {
	AllocationID string  `json:"allocation_id"`
	PublicIP     string  `json:"public_ip"`
	InstanceID   string  `json:"instance_id,omitempty"`
	State        State   `json:"state"`
	MonthlyWaste float64 `json:"monthly_waste_usd"`
}

// Idle reports whether this address is wasting money.
func (a Address) Idle() bool { return a.State.idle() }

// EC2API is the slice of the EC2 client the finder needs. Injecting it keeps
// the classifier unit-testable with no live AWS calls.
type EC2API interface {
	DescribeAddresses(ctx context.Context, in *ec2.DescribeAddressesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

// Report is the full scan result for a region.
type Report struct {
	Region         string    `json:"region"`
	Addresses      []Address `json:"addresses"`
	IdleCount      int       `json:"idle_count"`
	TotalMonthly   float64   `json:"total_monthly_waste_usd"`
	HourlyPriceUSD float64   `json:"hourly_price_usd"`
}

// Scan enumerates every allocated Elastic IP in the region, classifies each,
// and totals the monthly waste of the idle ones.
func Scan(ctx context.Context, client EC2API, region string) (*Report, error) {
	out, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, fmt.Errorf("describe addresses: %w", err)
	}

	running, err := runningInstances(ctx, client, out.Addresses)
	if err != nil {
		return nil, err
	}

	rep := &Report{Region: region, HourlyPriceUSD: HourlyPrice(region)}
	perIdle := MonthlyWaste(region)

	for _, a := range out.Addresses {
		addr := classify(a, running, perIdle)
		rep.Addresses = append(rep.Addresses, addr)
		if addr.Idle() {
			rep.IdleCount++
			rep.TotalMonthly += addr.MonthlyWaste
		}
	}

	// Deterministic ordering: idle first, then by public IP.
	sort.SliceStable(rep.Addresses, func(i, j int) bool {
		ai, aj := rep.Addresses[i], rep.Addresses[j]
		if ai.Idle() != aj.Idle() {
			return ai.Idle()
		}
		return ai.PublicIP < aj.PublicIP
	})
	return rep, nil
}

// runningInstances returns the set of instance IDs (referenced by any EIP) that
// are currently in the "running" state.
func runningInstances(ctx context.Context, client EC2API, addrs []ec2types.Address) (map[string]bool, error) {
	ids := map[string]struct{}{}
	for _, a := range addrs {
		if a.InstanceId != nil && aws.ToString(a.InstanceId) != "" {
			ids[aws.ToString(a.InstanceId)] = struct{}{}
		}
	}
	running := map[string]bool{}
	if len(ids) == 0 {
		return running, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	out, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: list})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}
	for _, r := range out.Reservations {
		for _, inst := range r.Instances {
			if inst.State != nil && inst.State.Name == ec2types.InstanceStateNameRunning {
				running[aws.ToString(inst.InstanceId)] = true
			}
		}
	}
	return running, nil
}

// classify turns a raw EC2 address into a normalized, priced Address.
func classify(a ec2types.Address, running map[string]bool, perIdleMonthly float64) Address {
	addr := Address{
		AllocationID: aws.ToString(a.AllocationId),
		PublicIP:     aws.ToString(a.PublicIp),
		InstanceID:   aws.ToString(a.InstanceId),
	}
	switch {
	case aws.ToString(a.AssociationId) == "":
		// No association at all — unattached.
		addr.State = StateUnattached
	case addr.InstanceID != "" && !running[addr.InstanceID]:
		// Associated with an instance that is not running.
		addr.State = StateAttachedToStopped
	case addr.InstanceID == "":
		// Associated (has AssociationId) but to a non-instance target such as a
		// network interface / NAT gateway — treat as active.
		addr.State = StateActive
	default:
		addr.State = StateActive
	}
	if addr.State.idle() {
		addr.MonthlyWaste = perIdleMonthly
	}
	return addr
}
