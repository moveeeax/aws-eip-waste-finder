package finder

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// mockEC2 is an injected EC2API for deterministic, offline tests.
type mockEC2 struct {
	addrs     []ec2types.Address
	instances map[string]ec2types.InstanceStateName
}

func (m *mockEC2) DescribeAddresses(_ context.Context, _ *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	return &ec2.DescribeAddressesOutput{Addresses: m.addrs}, nil
}

func (m *mockEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	var res []ec2types.Reservation
	for _, id := range in.InstanceIds {
		st, ok := m.instances[id]
		if !ok {
			continue
		}
		res = append(res, ec2types.Reservation{
			Instances: []ec2types.Instance{{
				InstanceId: aws.String(id),
				State:      &ec2types.InstanceState{Name: st},
			}},
		})
	}
	return &ec2.DescribeInstancesOutput{Reservations: res}, nil
}

func addr(pub, alloc, assoc, inst string) ec2types.Address {
	a := ec2types.Address{
		PublicIp:     aws.String(pub),
		AllocationId: aws.String(alloc),
	}
	if assoc != "" {
		a.AssociationId = aws.String(assoc)
	}
	if inst != "" {
		a.InstanceId = aws.String(inst)
	}
	return a
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		a    ec2types.Address
		run  map[string]bool
		want State
	}{
		{
			name: "unattached has no association",
			a:    addr("52.0.0.1", "eipalloc-1", "", ""),
			want: StateUnattached,
		},
		{
			name: "attached to running instance is active",
			a:    addr("52.0.0.2", "eipalloc-2", "eipassoc-2", "i-run"),
			run:  map[string]bool{"i-run": true},
			want: StateActive,
		},
		{
			name: "attached to stopped instance is idle",
			a:    addr("52.0.0.3", "eipalloc-3", "eipassoc-3", "i-stop"),
			run:  map[string]bool{},
			want: StateAttachedToStopped,
		},
		{
			name: "associated to non-instance target (e.g. ENI/NAT) is active",
			a:    addr("52.0.0.4", "eipalloc-4", "eipassoc-4", ""),
			want: StateActive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.a, tt.run, MonthlyWaste("eu-central-1"))
			if got.State != tt.want {
				t.Fatalf("state = %s, want %s", got.State, tt.want)
			}
			wantWaste := 0.0
			if tt.want.idle() {
				wantWaste = MonthlyWaste("eu-central-1")
			}
			if got.MonthlyWaste != wantWaste {
				t.Fatalf("waste = %.4f, want %.4f", got.MonthlyWaste, wantWaste)
			}
		})
	}
}

func TestScanTotalsAndOrdering(t *testing.T) {
	m := &mockEC2{
		addrs: []ec2types.Address{
			addr("52.0.0.9", "eipalloc-9", "eipassoc-9", "i-run"),  // active
			addr("52.0.0.1", "eipalloc-1", "", ""),                 // unattached (idle)
			addr("52.0.0.5", "eipalloc-5", "eipassoc-5", "i-stop"), // attached-stopped (idle)
		},
		instances: map[string]ec2types.InstanceStateName{
			"i-run":  ec2types.InstanceStateNameRunning,
			"i-stop": ec2types.InstanceStateNameStopped,
		},
	}
	rep, err := Scan(context.Background(), m, "eu-central-1")
	if err != nil {
		t.Fatal(err)
	}
	if rep.IdleCount != 2 {
		t.Fatalf("idle count = %d, want 2", rep.IdleCount)
	}
	want := 2 * MonthlyWaste("eu-central-1")
	if rep.TotalMonthly != want {
		t.Fatalf("total = %.4f, want %.4f", rep.TotalMonthly, want)
	}
	// Idle entries must sort before active ones.
	if !rep.Addresses[0].Idle() || !rep.Addresses[1].Idle() {
		t.Fatalf("idle addresses should sort first: %+v", rep.Addresses)
	}
	if rep.Addresses[2].State != StateActive {
		t.Fatalf("last should be active, got %s", rep.Addresses[2].State)
	}
	// Within idle, ordered by public IP.
	if rep.Addresses[0].PublicIP != "52.0.0.1" {
		t.Fatalf("idle not IP-ordered: %s", rep.Addresses[0].PublicIP)
	}
}

func TestPricingOverride(t *testing.T) {
	if got := HourlyPrice("eu-central-1"); got != defaultHourlyUSD {
		t.Fatalf("default price = %.4f, want %.4f", got, defaultHourlyUSD)
	}
	if got := MonthlyWaste("eu-central-1"); got != defaultHourlyUSD*hoursPerMonth {
		t.Fatalf("monthly = %.4f", got)
	}
}

func TestReportJSONRoundTrip(t *testing.T) {
	m := &mockEC2{addrs: []ec2types.Address{addr("52.0.0.1", "eipalloc-1", "", "")}}
	rep, err := Scan(context.Background(), m, "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := rep.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("json invalid: %v", err)
	}
	if back.IdleCount != 1 || back.Region != "us-east-1" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestReportTable(t *testing.T) {
	m := &mockEC2{addrs: []ec2types.Address{addr("52.0.0.1", "eipalloc-1", "", "")}}
	rep, _ := Scan(context.Background(), m, "us-east-1")
	var buf bytes.Buffer
	if err := rep.WriteTable(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "UNATTACHED") || !strings.Contains(out, "1 idle") {
		t.Fatalf("table missing expected content:\n%s", out)
	}
}
