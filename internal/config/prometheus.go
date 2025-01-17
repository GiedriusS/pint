package config

import "regexp"

type PrometheusConfig struct {
	Name    string   `hcl:",label"`
	URI     string   `hcl:"uri"`
	Timeout string   `hcl:"timeout"`
	Paths   []string `hcl:"paths,optional"`
}

func (pc PrometheusConfig) validate() error {
	if _, err := parseDuration(pc.Timeout); err != nil {
		return err
	}

	for _, path := range pc.Paths {
		if _, err := regexp.Compile(path); err != nil {
			return err
		}

	}

	return nil
}

func (pc PrometheusConfig) isEnabledForPath(path string) bool {
	if len(pc.Paths) == 0 {
		return true
	}
	for _, pattern := range pc.Paths {
		re := strictRegex(pattern)
		if re.MatchString(path) {
			return true
		}
	}
	return false
}
