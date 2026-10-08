package config

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Host      Host      `json:"host"`
	Sites     []Site    `json:"sites"`
	Discovery Discovery `json:"discovery,omitempty"`
}

type Discovery struct {
	Disabled      bool   `json:"disabled,omitempty"`
	CollectorFile string `json:"collector_file,omitempty"`
}

func (c Config) Discovering() bool { return !c.Discovery.Disabled }

type Host struct {
	SSHPort           int         `json:"ssh_port,omitempty"`
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	SSH               string      `json:"ssh,omitempty"`
	IdentityFile      string      `json:"identity_file,omitempty"`
	CaddyContainer    string      `json:"caddy_container,omitempty"`
	CaddyAdminURL     string      `json:"caddy_admin_url,omitempty"`
	MonthlyCost       float64     `json:"monthly_cost,omitempty"`
	MonthlyBackupCost float64     `json:"monthly_backup_cost,omitempty"`
	CostWeights       CostWeights `json:"cost_weights,omitempty"`
	Currency          string      `json:"currency,omitempty"`
	DiskRoots         []string    `json:"disk_roots,omitempty"`
}

type CostWeights struct {
	Equal  float64 `json:"equal"`
	Memory float64 `json:"memory"`
	CPU    float64 `json:"cpu"`
}

type Site struct {
	Dependencies []string `json:"dependencies,omitempty"`
	Discovered   bool     `json:"discovered,omitempty"`
	ID           string   `json:"id"`
	HostID       string   `json:"host_id"`
	Name         string   `json:"name"`
	Repository   string   `json:"repository,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Runtime      Runtime  `json:"runtime"`
	Proxy        Proxy    `json:"proxy"`
	Health       Health   `json:"health"`
	Business     Business `json:"business,omitempty"`
	Recovery     Recovery `json:"recovery,omitempty"`
}

type Runtime struct {
	Type          string   `json:"type"`
	Container     string   `json:"container,omitempty"`
	Network       string   `json:"network,omitempty"`
	Port          int      `json:"port,omitempty"`
	Directory     string   `json:"directory,omitempty"`
	ExpectedFiles []string `json:"expected_files,omitempty"`
	Unit          string   `json:"unit,omitempty"`
}

type Proxy struct {
	Domain      string   `json:"domain,omitempty"`
	Upstream    string   `json:"upstream,omitempty"`
	ExpectedIPs []string `json:"expected_ips,omitempty"`
}

type Health struct {
	Automatic      bool   `json:"automatic,omitempty"`
	Path           string `json:"path"`
	PublicURL      string `json:"public_url,omitempty"`
	LocalURL       string `json:"local_url,omitempty"`
	ExpectedStatus int    `json:"expected_status"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

type Business struct {
	Client     string  `json:"client,omitempty"`
	MonthlyFee float64 `json:"monthly_fee,omitempty"`
}

type Recovery struct {
	BackupDirectory string `json:"backup_directory,omitempty"`
	RTOminutes      int    `json:"rto_target_minutes,omitempty"`
	RPOhours        int    `json:"rpo_target_hours,omitempty"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var sshTarget = regexp.MustCompile(`^([a-zA-Z0-9_.-]+@)?[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024))
	decoder.DisallowUnknownFields()
	var c Config
	if err = decoder.Decode(&c); err != nil {
		return c, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("manifest must contain one JSON object")
	}
	return c, c.Validate()
}

func (c *Config) Validate() error {
	if !identifier.MatchString(c.Host.ID) {
		return fmt.Errorf("host.id must be an identifier")
	}
	if c.Host.SSH != "" && !validSSH(c.Host.SSH) {
		return fmt.Errorf("host.ssh must be a host or user@host, without options")
	}
	if c.Host.SSHPort < 0 || c.Host.SSHPort > 65535 {
		return fmt.Errorf("SSH port must be 1..65535, or zero for the SSH default")
	}
	if c.Host.CaddyContainer != "" && !identifier.MatchString(c.Host.CaddyContainer) {
		return fmt.Errorf("invalid Caddy container")
	}
	if len(c.Host.DiskRoots) > 16 {
		return fmt.Errorf("at most 16 disk roots may be configured")
	}
	for _, root := range c.Host.DiskRoots {
		if !strings.HasPrefix(root, "/") || strings.ContainsRune(root, '\x00') || strings.ContainsAny(root, "\r\n") {
			return fmt.Errorf("disk roots must be absolute Linux paths")
		}
	}
	if c.Host.CaddyAdminURL != "" {
		u, err := url.Parse(c.Host.CaddyAdminURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("Caddy admin URL must be an HTTP(S) origin without credentials")
		}
		c.Host.CaddyAdminURL = strings.TrimSuffix(c.Host.CaddyAdminURL, "/")
	}
	if c.Host.MonthlyCost < 0 {
		return fmt.Errorf("host cost cannot be negative")
	}
	if c.Host.MonthlyBackupCost < 0 {
		return fmt.Errorf("backup cost cannot be negative")
	}
	w := c.Host.CostWeights
	sum := w.Equal + w.Memory + w.CPU
	if w.Equal < 0 || w.Memory < 0 || w.CPU < 0 || sum != 0 && math.Abs(sum-1) > 0.000001 {
		return fmt.Errorf("cost weights must be non-negative and total 1")
	}
	if c.Host.Currency != "" && !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(c.Host.Currency) {
		return fmt.Errorf("currency must be a three-letter ISO code")
	}
	if c.Host.MonthlyCost+c.Host.MonthlyBackupCost > 0 && c.Host.Currency == "" {
		return fmt.Errorf("configured costs require a currency")
	}
	if len(c.Sites) == 0 && c.Discovery.Disabled {
		return fmt.Errorf("manifest must register at least one site")
	}
	seen := map[string]bool{}
	for i := range c.Sites {
		s := &c.Sites[i]
		if s.Recovery.RTOminutes < 0 || s.Recovery.RTOminutes > 525600 || s.Recovery.RPOhours < 0 || s.Recovery.RPOhours > 8760 {
			return fmt.Errorf("site %s: recovery targets must be between zero and one year", s.ID)
		}
		if s.Business.MonthlyFee < 0 {
			return fmt.Errorf("site %s: hosting fee cannot be negative", s.ID)
		}
		if !identifier.MatchString(s.ID) || seen[s.ID] {
			return fmt.Errorf("invalid or duplicate site id %q", s.ID)
		}
		seen[s.ID] = true
		if s.HostID != c.Host.ID {
			return fmt.Errorf("site %s host_id must match host.id", s.ID)
		}
		if s.Name == "" {
			return fmt.Errorf("site %s requires a name", s.ID)
		}
		switch s.Runtime.Type {
		case "docker":
			if !identifier.MatchString(s.Runtime.Container) {
				return fmt.Errorf("site %s: invalid container", s.ID)
			}
			if s.Runtime.Port < 0 || s.Runtime.Port > 65535 {
				return fmt.Errorf("site %s: invalid runtime port", s.ID)
			}
		case "static":
			if !strings.HasPrefix(s.Runtime.Directory, "/") {
				return fmt.Errorf("site %s: static directory must be absolute", s.ID)
			}
			for _, name := range s.Runtime.ExpectedFiles {
				if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") || strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
					return fmt.Errorf("site %s: expected_files must be relative paths without traversal", s.ID)
				}
			}
		case "systemd":
			if !identifier.MatchString(s.Runtime.Unit) {
				return fmt.Errorf("site %s: invalid systemd unit", s.ID)
			}
		default:
			return fmt.Errorf("site %s: unsupported runtime %q", s.ID, s.Runtime.Type)
		}
		if s.Health.ExpectedStatus == 0 && !s.Health.Automatic {
			s.Health.ExpectedStatus = 200
		}
		if s.Health.TimeoutSeconds == 0 {
			s.Health.TimeoutSeconds = 5
		}
		if s.Health.TimeoutSeconds < 1 || s.Health.TimeoutSeconds > 60 {
			return fmt.Errorf("site %s: timeout must be 1..60 seconds", s.ID)
		}
		if (!s.Health.Automatic && s.Health.ExpectedStatus < 100) || s.Health.ExpectedStatus > 599 {
			return fmt.Errorf("site %s: invalid expected HTTP status", s.ID)
		}
		if s.Health.Path == "" {
			s.Health.Path = "/"
		}
		if s.Health.Path[0] != '/' {
			return fmt.Errorf("site %s: health path must begin with /", s.ID)
		}
		for _, raw := range []string{s.Health.LocalURL, s.Health.PublicURL} {
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
				return fmt.Errorf("site %s: health URLs must be http(s) without credentials, query or fragment", s.ID)
			}
		}
	}
	return nil
}

func (s Site) Timeout() time.Duration { return time.Duration(s.Health.TimeoutSeconds) * time.Second }

func validSSH(target string) bool {
	if sshTarget.MatchString(target) {
		return true
	}
	parts := strings.Split(target, "@")
	if len(parts) > 2 {
		return false
	}
	host := parts[len(parts)-1]
	if len(parts) == 2 && !identifier.MatchString(parts[0]) {
		return false
	}
	return net.ParseIP(strings.Trim(host, "[]")) != nil
}

func (c Config) Select(id string) ([]Site, error) {
	if id == "" {
		return c.Sites, nil
	}
	for _, s := range c.Sites {
		if s.ID == id {
			return []Site{s}, nil
		}
	}
	return nil, fmt.Errorf("site %q is not registered", id)
}
