package finder

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteJSON renders the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteTable renders a human-readable table plus a summary line.
func (r *Report) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tPUBLIC_IP\tALLOCATION_ID\tINSTANCE\tCOST_$/MO\tWASTE_$/MO")
	for _, a := range r.Addresses {
		inst := a.InstanceID
		if inst == "" {
			inst = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.2f\t%.2f\n",
			a.State, a.PublicIP, a.AllocationID, inst, a.MonthlyCost, a.MonthlyWaste)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\n%d allocated, %d idle — estimated waste $%.2f/mo of $%.2f/mo billed (region %s @ $%.4f/hr per public IPv4)\n",
		len(r.Addresses), r.IdleCount, r.TotalMonthly, r.TotalMonthlyCost, r.Region, r.HourlyPriceUSD)
	return nil
}
