// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// rawMap builds a tftypes map value (or null) for use as a State/Plan raw value.
func rawMap(null bool, elems map[string]string) tftypes.Value {
	mt := tftypes.Map{ElementType: tftypes.String}
	if null {
		return tftypes.NewValue(mt, nil)
	}
	vals := map[string]tftypes.Value{}
	for k, v := range elems {
		vals[k] = tftypes.NewValue(tftypes.String, v)
	}
	return tftypes.NewValue(mt, vals)
}

func tfMap(t *testing.T, null bool, elems map[string]string) types.Map {
	t.Helper()
	if null {
		return types.MapNull(types.StringType)
	}
	m := map[string]string{}
	for k, v := range elems {
		m[k] = v
	}
	mv, diags := types.MapValueFrom(context.Background(), types.StringType, m)
	if diags.HasError() {
		t.Fatalf("building map: %v", diags)
	}
	return mv
}

func TestMapRequiresReplaceIfConfigured(t *testing.T) {
	ctx := context.Background()
	mod := mapRequiresReplaceIfConfigured()

	tests := []struct {
		name        string
		stateRaw    tftypes.Value
		planRaw     tftypes.Value
		stateValue  types.Map
		planValue   types.Map
		wantReplace bool
	}{
		{
			name:        "create (state null)",
			stateRaw:    rawMap(true, nil),
			planRaw:     rawMap(false, map[string]string{"a": "1"}),
			stateValue:  tfMap(t, true, nil),
			planValue:   tfMap(t, false, map[string]string{"a": "1"}),
			wantReplace: false,
		},
		{
			name:        "destroy (plan null)",
			stateRaw:    rawMap(false, map[string]string{"a": "1"}),
			planRaw:     rawMap(true, nil),
			stateValue:  tfMap(t, false, map[string]string{"a": "1"}),
			planValue:   tfMap(t, true, nil),
			wantReplace: false,
		},
		{
			name:        "unchanged",
			stateRaw:    rawMap(false, map[string]string{"a": "1"}),
			planRaw:     rawMap(false, map[string]string{"a": "1"}),
			stateValue:  tfMap(t, false, map[string]string{"a": "1"}),
			planValue:   tfMap(t, false, map[string]string{"a": "1"}),
			wantReplace: false,
		},
		{
			name:        "changed",
			stateRaw:    rawMap(false, map[string]string{"a": "1"}),
			planRaw:     rawMap(false, map[string]string{"a": "2"}),
			stateValue:  tfMap(t, false, map[string]string{"a": "1"}),
			planValue:   tfMap(t, false, map[string]string{"a": "2"}),
			wantReplace: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.MapRequest{
				State:      tfsdk.State{Raw: tc.stateRaw},
				Plan:       tfsdk.Plan{Raw: tc.planRaw},
				StateValue: tc.stateValue,
				PlanValue:  tc.planValue,
			}
			resp := &planmodifier.MapResponse{}
			mod.PlanModifyMap(ctx, req, resp)
			if resp.RequiresReplace != tc.wantReplace {
				t.Errorf("RequiresReplace = %v, want %v", resp.RequiresReplace, tc.wantReplace)
			}
		})
	}
}
