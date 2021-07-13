package config

import (
	"fmt"
	"regexp"
	"time"

	"github.com/cloudflare/pint/internal/checks"
	"github.com/cloudflare/pint/internal/parser"
	"github.com/rs/zerolog/log"
)

var (
	alertingRuleType  = "alerting"
	recordingRuleType = "recording"
)

type MatchLabel struct {
	Key             string `hcl:",label"`
	Value           string `hcl:"value"`
	annotationCheck bool
}

func (ml MatchLabel) validate() error {
	if _, err := regexp.Compile(ml.Key); err != nil {
		return err
	}
	if _, err := regexp.Compile(ml.Value); err != nil {
		return err
	}
	return nil
}

func (ml MatchLabel) isMatching(rule parser.Rule) bool {
	keyRe := strictRegex(ml.Key)
	valRe := strictRegex(ml.Value)

	if ml.annotationCheck {
		if rule.AlertingRule == nil || rule.AlertingRule.Annotations == nil {
			return false
		}
		for _, ann := range rule.AlertingRule.Annotations.Items {
			if keyRe.MatchString(ann.Key.Value) && valRe.MatchString(ann.Value.Value) {
				return true
			}
		}
		return false
	}

	if rule.AlertingRule != nil {
		if rule.AlertingRule.Labels != nil {
			for _, labl := range rule.AlertingRule.Labels.Items {
				if keyRe.MatchString(labl.Key.Value) && valRe.MatchString(labl.Value.Value) {
					return true
				}
			}
		}
	}
	if rule.RecordingRule != nil {
		if rule.RecordingRule.Labels != nil {
			for _, labl := range rule.RecordingRule.Labels.Items {
				if keyRe.MatchString(labl.Key.Value) && valRe.MatchString(labl.Value.Value) {
					return true
				}
			}
		}
	}

	return false
}

type Match struct {
	Path       string      `hcl:"path,optional"`
	Kind       string      `hcl:"kind,optional"`
	Label      *MatchLabel `hcl:"label,block"`
	Annotation *MatchLabel `hcl:"annotation,block"`
}

func (m Match) validate() error {
	if _, err := regexp.Compile(m.Path); err != nil {
		return err
	}

	switch m.Kind {
	case "":
		// not set
	case alertingRuleType, recordingRuleType:
		// pass
	default:
		return fmt.Errorf("unknown rule type: %s", m.Kind)
	}

	if m.Label != nil {
		if err := m.Label.validate(); err != nil {
			return nil
		}
	}

	return nil
}

type Rule struct {
	Match      *Match               `hcl:"match,block"`
	Aggregate  []AggregateSettings  `hcl:"aggregate,block"`
	Rate       *RateSettings        `hcl:"rate,block"`
	Annotation []AnnotationSettings `hcl:"annotation,block"`
	Label      []AnnotationSettings `hcl:"label,block"`
	Series     *SeriesSettings      `hcl:"series,block"`
	Cost       *CostSettings        `hcl:"cost,block"`
	Alerts     *AlertsSettings      `hcl:"alerts,block"`
	Value      *ValueSettings       `hcl:"value,block"`
	Reject     []RejectSettings     `hcl:"reject,block"`
}

func (rule Rule) resolveChecks(path string, r parser.Rule, enabledChecks, disabledChecks []string, proms []PrometheusConfig, recordingRules *[]*parser.RecordingRule) []checks.RuleChecker {
	enabled := []checks.RuleChecker{}

	if rule.Match != nil && rule.Match.Kind != "" {
		var isAllowed bool
		recordingEnabled := rule.Match.Kind == recordingRuleType
		alertingEnabled := rule.Match.Kind == alertingRuleType
		if r.AlertingRule != nil && alertingEnabled {
			isAllowed = true
		}
		if r.RecordingRule != nil && recordingEnabled {
			isAllowed = true
		}
		if !isAllowed {
			return enabled
		}
	}

	if rule.Match != nil {
		if rule.Match.Path != "" {
			re := strictRegex(rule.Match.Path)
			if !re.MatchString(path) {
				return enabled
			}
		}

		if rule.Match.Label != nil {
			if !rule.Match.Label.isMatching(r) {
				return enabled
			}
		}
	}

	if len(rule.Aggregate) > 0 {
		var nameRegex *regexp.Regexp
		for _, aggr := range rule.Aggregate {
			if aggr.Name != "" {
				nameRegex = strictRegex(aggr.Name)
			}
			severity := aggr.getSeverity(checks.Warning)
			for _, label := range aggr.Keep {
				if isEnabled(enabledChecks, disabledChecks, checks.WithoutCheckName, r) {
					enabled = append(enabled, checks.NewWithoutCheck(nameRegex, label, true, severity))
				}
				if isEnabled(enabledChecks, disabledChecks, checks.ByCheckName, r) {
					enabled = append(enabled, checks.NewByCheck(nameRegex, label, true, severity))
				}
			}
			for _, label := range aggr.Strip {
				if isEnabled(enabledChecks, disabledChecks, checks.WithoutCheckName, r) {
					enabled = append(enabled, checks.NewWithoutCheck(nameRegex, label, false, severity))
				}
				if isEnabled(enabledChecks, disabledChecks, checks.ByCheckName, r) {
					enabled = append(enabled, checks.NewByCheck(nameRegex, label, false, severity))
				}
			}
		}
	}

	if rule.Rate != nil && isEnabled(enabledChecks, disabledChecks, checks.RateCheckName, r) {
		for _, prom := range proms {
			timeout, _ := parseDuration(prom.Timeout)
			enabled = append(enabled, checks.NewRateCheck(prom.Name, prom.URI, timeout))
		}
	}

	if rule.Cost != nil && isEnabled(enabledChecks, disabledChecks, checks.CostCheckName, r) {
		severity := rule.Cost.getSeverity(checks.Bug)
		for _, prom := range proms {
			timeout, _ := parseDuration(prom.Timeout)
			enabled = append(enabled, checks.NewCostCheck(prom.Name, prom.URI, timeout, rule.Cost.BytesPerSample, rule.Cost.MaxSeries, severity))
		}
	}

	if len(rule.Annotation) > 0 && isEnabled(enabledChecks, disabledChecks, checks.AnnotationCheckName, r) {
		for _, ann := range rule.Annotation {
			var valueRegex *regexp.Regexp
			if ann.Value != "" {
				valueRegex = strictRegex(ann.Value)
			}
			severity := ann.getSeverity(checks.Warning)
			enabled = append(enabled, checks.NewAnnotationCheck(ann.Key, valueRegex, ann.Required, severity))
		}
	}
	if len(rule.Label) > 0 && isEnabled(enabledChecks, disabledChecks, checks.LabelCheckName, r) {
		for _, lab := range rule.Label {
			var valueRegex *regexp.Regexp
			if lab.Value != "" {
				valueRegex = strictRegex(lab.Value)
			}
			severity := lab.getSeverity(checks.Warning)
			enabled = append(enabled, checks.NewLabelCheck(lab.Key, valueRegex, lab.Required, severity))
		}
	}

	if rule.Series != nil && isEnabled(enabledChecks, disabledChecks, checks.SeriesCheckName, r) {
		severity := rule.Series.getSeverity(checks.Warning)
		for _, prom := range proms {
			timeout, _ := parseDuration(prom.Timeout)
			enabled = append(enabled, checks.NewSeriesCheck(prom.Name, prom.URI, timeout, severity, rule.Series.IgnoreRR, recordingRules))
		}
	}

	if rule.Alerts != nil && isEnabled(enabledChecks, disabledChecks, checks.AlertsCheckName, r) {
		qRange := time.Hour * 24
		if rule.Alerts.Range != "" {
			qRange, _ = parseDuration(rule.Alerts.Range)
		}
		qStep := time.Minute
		if rule.Alerts.Step != "" {
			qStep, _ = parseDuration(rule.Alerts.Step)
		}
		qResolve := time.Minute * 5
		if rule.Alerts.Resolve != "" {
			qResolve, _ = parseDuration(rule.Alerts.Resolve)
		}
		for _, prom := range proms {
			timeout, _ := parseDuration(prom.Timeout)
			enabled = append(enabled, checks.NewAlertsCheck(prom.Name, prom.URI, timeout, qRange, qStep, qResolve))
		}
	}

	if rule.Value != nil && isEnabled(enabledChecks, disabledChecks, checks.ValueCheckName, r) {
		severity := rule.Value.getSeverity(checks.Bug)
		enabled = append(enabled, checks.NewValueCheck(severity))
	}

	if len(rule.Reject) > 0 && isEnabled(enabledChecks, disabledChecks, checks.RejectCheckName, r) {
		for _, reject := range rule.Reject {
			severity := reject.getSeverity(checks.Bug)
			if reject.LabelKeys {
				re := strictRegex(reject.Regex)
				enabled = append(enabled, checks.NewRejectCheck(true, false, re, nil, severity))
			}
			if reject.LabelValues {
				re := strictRegex(reject.Regex)
				enabled = append(enabled, checks.NewRejectCheck(true, false, nil, re, severity))
			}
			if reject.AnnotationKeys {
				re := strictRegex(reject.Regex)
				enabled = append(enabled, checks.NewRejectCheck(false, true, re, nil, severity))
			}
			if reject.AnnotationValues {
				re := strictRegex(reject.Regex)
				enabled = append(enabled, checks.NewRejectCheck(false, true, nil, re, severity))
			}
		}
	}

	return enabled
}

func isEnabled(enabledChecks, disabledChecks []string, name string, rule parser.Rule) bool {
	if rule.HasComment(fmt.Sprintf("disable %s", removeRedundantSpaces(name))) {
		log.Debug().
			Str("check", name).
			Msg("Check disabled by comment")
		return false
	}

	for _, c := range disabledChecks {
		if c == name {
			return false
		}
	}
	if len(enabledChecks) == 0 {
		return true
	}
	for _, c := range enabledChecks {
		if c == name {
			return true
		}
	}
	return false
}

func strictRegex(s string) *regexp.Regexp {
	return regexp.MustCompile("^" + s + "$")
}
