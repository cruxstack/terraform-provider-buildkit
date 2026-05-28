// Copyright (c) Cruxstack
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// requiresReplaceIfConfiguredMap forces resource replacement when a configured
// map attribute changes. Unlike a plain RequiresReplace, it does not trigger a
// replace when the attribute is null in both config and plan.
type requiresReplaceIfConfiguredMap struct{}

func mapRequiresReplaceIfConfigured() planmodifier.Map {
	return requiresReplaceIfConfiguredMap{}
}

func (m requiresReplaceIfConfiguredMap) Description(_ context.Context) string {
	return "If the value of this attribute changes, Terraform will destroy and recreate the resource."
}

func (m requiresReplaceIfConfiguredMap) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m requiresReplaceIfConfiguredMap) PlanModifyMap(ctx context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	// no state => create; nothing to replace.
	if req.State.Raw.IsNull() {
		return
	}
	// no plan => destroy; nothing to replace.
	if req.Plan.Raw.IsNull() {
		return
	}
	// unknown plan value cannot be compared yet.
	if req.PlanValue.IsUnknown() {
		return
	}
	if req.StateValue.Equal(req.PlanValue) {
		return
	}
	resp.RequiresReplace = true
}
