package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type SystemSettings struct {
	Comment             *string             `json:"comment,omitempty"`
	GCGraceSeconds      *int64              `json:"gc_grace_seconds,omitempty"`
	DefaultTTL          *int64              `json:"default_time_to_live,omitempty"`
	SpeculativeRetry    *string             `json:"speculative_retry,omitempty"`
	BloomFilterFPChance *float64            `json:"bloom_filter_fp_chance,omitempty"`
	Compaction          *CompactionSettings `json:"compaction,omitempty"`
	AdditionalOptions   map[string]string   `json:"additional_options,omitempty"`
}

type CompactionSettings struct {
	Class   string            `json:"class"`
	Options map[string]string `json:"options,omitempty"`
}

func extractSystemSettingsFromModel(ctx context.Context, model SystemLevelTableSettingsModel) (SystemSettings, error) {
	settings := SystemSettings{}

	if !model.Comment.IsNull() && !model.Comment.IsUnknown() {
		value := model.Comment.ValueString()
		settings.Comment = &value
	}
	if !model.GCGraceSeconds.IsNull() && !model.GCGraceSeconds.IsUnknown() {
		value := model.GCGraceSeconds.ValueInt64()
		settings.GCGraceSeconds = &value
	}
	if !model.DefaultTTL.IsNull() && !model.DefaultTTL.IsUnknown() {
		value := model.DefaultTTL.ValueInt64()
		settings.DefaultTTL = &value
	}
	if !model.SpeculativeRetry.IsNull() && !model.SpeculativeRetry.IsUnknown() {
		value := model.SpeculativeRetry.ValueString()
		settings.SpeculativeRetry = &value
	}
	if !model.BloomFilterFPChance.IsNull() && !model.BloomFilterFPChance.IsUnknown() {
		value := model.BloomFilterFPChance.ValueFloat64()
		settings.BloomFilterFPChance = &value
	}

	if !model.Compaction.IsNull() && !model.Compaction.IsUnknown() {
		var compaction CompactionModel
		diags := model.Compaction.As(ctx, &compaction, basetypes.ObjectAsOptions{})
		if diags.HasError() {
			return SystemSettings{}, fmt.Errorf("unable to decode compaction block")
		}

		compactionSettings := CompactionSettings{
			Class: compaction.Class.ValueString(),
		}
		if !compaction.Options.IsNull() && !compaction.Options.IsUnknown() {
			options := make(map[string]string)
			diags = compaction.Options.ElementsAs(ctx, &options, false)
			if diags.HasError() {
				return SystemSettings{}, fmt.Errorf("unable to decode compaction options")
			}
			compactionSettings.Options = options
		}
		settings.Compaction = &compactionSettings
	}

	if !model.AdditionalOptions.IsNull() && !model.AdditionalOptions.IsUnknown() {
		additional := make(map[string]string)
		diags := model.AdditionalOptions.ElementsAs(ctx, &additional, false)
		if diags.HasError() {
			return SystemSettings{}, fmt.Errorf("unable to decode additional_options")
		}
		settings.AdditionalOptions = additional
	}

	return settings, nil
}

func buildAlterTableSettingsStatement(keyspace, table string, settings SystemSettings) (string, error) {
	clauses, err := buildSystemSettingsClauses(settings)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ALTER TABLE %s WITH %s", qualifiedTableName(keyspace, table), strings.Join(clauses, " AND ")), nil
}

func appendSystemSettingsToCreateStatement(base string, settings SystemSettings) (string, error) {
	clauses, err := buildSystemSettingsClauses(settings)
	if err != nil {
		return "", err
	}
	if len(clauses) == 0 {
		return base, nil
	}

	separator := " WITH "
	if strings.Contains(base, " WITH ") {
		separator = " AND "
	}

	return base + separator + strings.Join(clauses, " AND "), nil
}

func buildSystemSettingsClauses(settings SystemSettings) ([]string, error) {
	clauses := make([]string, 0, 6)

	if settings.Comment != nil {
		clauses = append(clauses, fmt.Sprintf("comment = %s", quoteStringLiteral(*settings.Comment)))
	}
	if settings.GCGraceSeconds != nil {
		clauses = append(clauses, fmt.Sprintf("gc_grace_seconds = %d", *settings.GCGraceSeconds))
	}
	if settings.DefaultTTL != nil {
		clauses = append(clauses, fmt.Sprintf("default_time_to_live = %d", *settings.DefaultTTL))
	}
	if settings.SpeculativeRetry != nil {
		clauses = append(clauses, fmt.Sprintf("speculative_retry = %s", quoteStringLiteral(*settings.SpeculativeRetry)))
	}
	if settings.BloomFilterFPChance != nil {
		clauses = append(clauses, fmt.Sprintf("bloom_filter_fp_chance = %v", *settings.BloomFilterFPChance))
	}
	if settings.Compaction != nil {
		clauses = append(clauses, "compaction = "+buildCompactionLiteral(*settings.Compaction))
	}
	if len(settings.AdditionalOptions) > 0 {
		keys := make([]string, 0, len(settings.AdditionalOptions))
		for key := range settings.AdditionalOptions {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			clauses = append(clauses, fmt.Sprintf("%s = %s", key, quoteStringLiteral(settings.AdditionalOptions[key])))
		}
	}

	if len(clauses) == 0 {
		return nil, fmt.Errorf("at least one system-level setting must be configured")
	}

	return clauses, nil
}

func buildCompactionLiteral(compaction CompactionSettings) string {
	options := map[string]string{
		"class": compaction.Class,
	}
	for key, value := range compaction.Options {
		options[key] = value
	}

	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", quoteStringLiteral(key), quoteStringLiteral(options[key])))
	}

	return "{" + strings.Join(parts, ", ") + "}"
}
