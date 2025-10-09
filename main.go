package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cryzeck/irrdb"
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Irrdb        string       `yaml:"irrdb"`
	Host         string       `yaml:"host"`
	Port         string       `yaml:"port"`
	ApiKey       string       `yaml:"apikey"`
	Peers        []PeerConfig `yaml:"peers"`
	Mode         string       `yaml:"mode"`
	OutDir       string       `yaml:"output_dir"`
	Vendors      []string     `yaml:"vendors"`
	TemplateDir  string       `yaml:"template_dir"`
	PeeringDBURL string       `yaml:"peeringdb_url"`
}

type PeerConfig struct {
	Name         string   `yaml:"name"`
	ASN          int      `yaml:"asn"`
	ASSet        string   `yaml:"as_set"`
	NeighborIPv4 string   `yaml:"neighbor_ipv4"`
	NeighborIPv6 string   `yaml:"neighbor_ipv6"`
	Vendors      []string `yaml:"vendors"`
}

func (p *PeerConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw interface{}
	if err := unmarshal(&raw); err != nil {
		return err
	}
	switch value := raw.(type) {
	case string:
		p.Name = strings.TrimSpace(value)
		return nil
	case map[interface{}]interface{}:
		normalized := make(map[string]interface{}, len(value))
		for key, v := range value {
			keyStr, ok := key.(string)
			if !ok {
				return fmt.Errorf("invalid peer key %v (expected string)", key)
			}
			normalized[keyStr] = v
		}
		data, err := yaml.Marshal(normalized)
		if err != nil {
			return err
		}
		type rawPeer PeerConfig
		var aux rawPeer
		if err := yaml.Unmarshal(data, &aux); err != nil {
			return err
		}
		*p = PeerConfig(aux)
		return nil
	default:
		return fmt.Errorf("invalid peer definition: %v", raw)
	}
}

const (
	modeVyOS = "vyos"
)

var supportedVendors = map[string]string{
	"juniper":  "juniper.j2",
	"cisco":    "cisco.j2",
	"mikrotik": "mikrotik.j2",
	"arista":   "arista.j2",
}

type peeringDBResponse struct {
	Data []struct {
		ASN           int    `json:"asn"`
		Name          string `json:"name"`
		InfoPrefixes4 *int   `json:"info_prefixes4"`
		InfoPrefixes6 *int   `json:"info_prefixes6"`
		IrrASSet      string `json:"irr_as_set"`
	} `json:"data"`
}

type peeringDBLimits struct {
	Prefixes4 int
	Prefixes6 int
	Name      string
	IrrASSet  string
}

type templateRenderer struct {
	dir   string
	cache map[string]*jinjaTemplate
}

func newTemplateRenderer(dir string) (*templateRenderer, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("template path %q is not a directory", dir)
	}
	return &templateRenderer{dir: dir, cache: make(map[string]*jinjaTemplate)}, nil
}

func (r *templateRenderer) Render(templateName string, context map[string]interface{}) (string, error) {
	tpl, ok := r.cache[templateName]
	if !ok {
		path := filepath.Join(r.dir, templateName)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		tpl, err = parseJinjaTemplate(string(data))
		if err != nil {
			return "", err
		}
		r.cache[templateName] = tpl
	}
	return tpl.Render(context)
}

func fetchPeeringDBLimits(baseURL string, asn int) (*peeringDBLimits, error) {
	if asn <= 0 {
		return nil, errors.New("asn must be greater than zero")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://www.peeringdb.com/api"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	reqURL := fmt.Sprintf("%s/net?asn=%d", baseURL, asn)
	resp, err := http.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peeringdb lookup failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload peeringDBResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("no PeeringDB entry found for AS%d", asn)
	}
	entry := payload.Data[0]
	limits := &peeringDBLimits{Name: entry.Name, IrrASSet: entry.IrrASSet}
	if entry.InfoPrefixes4 != nil {
		limits.Prefixes4 = *entry.InfoPrefixes4
	}
	if entry.InfoPrefixes6 != nil {
		limits.Prefixes6 = *entry.InfoPrefixes6
	}
	return limits, nil
}

func (p PeerConfig) displayName() string {
	if p.Name != "" {
		return p.Name
	}
	if p.ASSet != "" {
		return p.ASSet
	}
	if p.ASN > 0 {
		return fmt.Sprintf("AS%d", p.ASN)
	}
	return "peer"
}

func (p PeerConfig) effectiveASSet() string {
	if strings.TrimSpace(p.ASSet) != "" {
		return strings.TrimSpace(p.ASSet)
	}
	if strings.TrimSpace(p.Name) != "" {
		return strings.TrimSpace(p.Name)
	}
	if p.ASN > 0 {
		return fmt.Sprintf("AS%d", p.ASN)
	}
	return ""
}

func (p *PeerConfig) deriveASNFromName() {
	if p.ASN != 0 {
		return
	}
	candidate := strings.ToUpper(strings.TrimSpace(p.Name))
	if candidate == "" {
		candidate = strings.ToUpper(strings.TrimSpace(p.ASSet))
	}
	if !strings.HasPrefix(candidate, "AS") {
		return
	}
	candidate = strings.TrimPrefix(candidate, "AS")
	candidate = strings.TrimLeft(candidate, "-")
	if candidate == "" {
		return
	}
	if asn, err := strconv.Atoi(candidate); err == nil {
		p.ASN = asn
	}
}

func (p PeerConfig) effectiveVendors(global []string, mode string) []string {
	vendors := append([]string{}, p.Vendors...)
	if len(vendors) == 0 {
		vendors = append(vendors, global...)
	}
	if len(vendors) == 0 && mode != modeVyOS {
		vendors = []string{mode}
	}
	return uniqueStrings(vendors)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizePrefixes(prefixes []string) []string {
	if len(prefixes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(prefixes))
	result := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		if _, ok := seen[prefix]; ok {
			continue
		}
		seen[prefix] = struct{}{}
		result = append(result, prefix)
	}
	sort.Strings(result)
	return result
}

func sanitizeFilenameComponent(component string) string {
	component = strings.ToLower(strings.TrimSpace(component))
	if component == "" {
		return "peer"
	}
	var builder strings.Builder
	for _, r := range component {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	sanitized := builder.String()
	sanitized = strings.Trim(sanitized, "_")
	if sanitized == "" {
		sanitized = "peer"
	}
	return sanitized
}

func writeTemplateOutput(outDir string, peer PeerConfig, vendor, content string) error {
	filenameBase := sanitizeFilenameComponent(peer.displayName())
	if outDir == "" {
		header := fmt.Sprintf("# ==== %s (%s) ====", peer.displayName(), vendor)
		fmt.Println(header)
		fmt.Println(strings.TrimSpace(content))
		fmt.Println()
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	filename := fmt.Sprintf("%s_%s.conf", filenameBase, vendor)
	path := filepath.Join(outDir, filename)
	content = strings.TrimSpace(content) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func buildTemplateContext(peer PeerConfig, vendor string, prefixes4, prefixes6 []string, limits *peeringDBLimits) map[string]interface{} {
	prefixLimit4 := 0
	prefixLimit6 := 0
	peeringDBName := ""
	peeringDBASSet := ""
	if limits != nil {
		prefixLimit4 = limits.Prefixes4
		prefixLimit6 = limits.Prefixes6
		peeringDBName = limits.Name
		peeringDBASSet = limits.IrrASSet
	}

	identifier := sanitizeFilenameComponent(peer.displayName())

	ctx := map[string]interface{}{
		"vendor":          vendor,
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"prefixes_v4":     prefixes4,
		"prefixes_v6":     prefixes6,
		"has_ipv4":        len(prefixes4) > 0,
		"has_ipv6":        len(prefixes6) > 0,
		"prefix_limit_v4": prefixLimit4,
		"prefix_limit_v6": prefixLimit6,
		"peer_identifier": identifier,
		"prefix_list_v4":  identifier + "-v4",
		"prefix_list_v6":  identifier + "-v6",
		"peer": map[string]interface{}{
			"name":             peer.displayName(),
			"asn":              peer.ASN,
			"as_set":           peer.effectiveASSet(),
			"neighbor_ipv4":    peer.NeighborIPv4,
			"neighbor_ipv6":    peer.NeighborIPv6,
			"peeringdb_name":   peeringDBName,
			"peeringdb_as_set": peeringDBASSet,
		},
	}
	return ctx
}

func ReadConfig(configPath string) (*Config, error) {
	config := &Config{}
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}

	defer file.Close()
	d := yaml.NewDecoder(file)

	if err := d.Decode(&config); err != nil {
		return nil, err
	}
	for i := range config.Peers {
		config.Peers[i].deriveASNFromName()
	}
	return config, nil
}

type policy struct {
	Op    string   `json:"op"`
	Path  []string `json:"path"`
	Value string   `json:"value"`
}

func NewPolicy(op string, path ...string) *policy {
	return &policy{op, path, ""}
}

func (p *policy) SetValue(value string) *policy {
	p.Value = value
	return p
}

func (p *policy) CloneExtend(path ...string) *policy {
	lis := make([]string, len(p.Path)+len(path))
	copy(lis, p.Path)
	copy(lis[len(p.Path):], path)
	return &policy{p.Op, lis, p.Value}
}

func CreatePolicies(name string, input []string) []*policy {
	policies := make([]*policy, 0, len(input)*2+1)
	policies = append(policies, NewPolicy("delete", "policy", "prefix-list").SetValue(name))

	rule := NewPolicy("set", "policy", "prefix-list", name, "rule")
	for i, value := range input {
		key := strconv.Itoa(i + 1)

		policies = append(policies,
			rule.CloneExtend(key, "action").SetValue("permit"),
			rule.CloneExtend(key, "prefix").SetValue(value),
		)
	}

	return policies
}

func CreatePolicies6(name string, input []string) []*policy {
	policies := make([]*policy, 0, len(input)*2+1)
	policies = append(policies, NewPolicy("delete", "policy", "prefix-list6").SetValue(name))

	rule := NewPolicy("set", "policy", "prefix-list6", name, "rule")
	for i, value := range input {
		key := strconv.Itoa(i + 1)

		policies = append(policies,
			rule.CloneExtend(key, "action").SetValue("permit"),
			rule.CloneExtend(key, "prefix").SetValue(value),
		)
	}

	return policies
}

func UpdateFilter(name string, pref []string, host, port, key string) {
	pref = normalizePrefixes(pref)
	if strings.Contains(name, "v6") {
		e, err := json.Marshal(CreatePolicies6(name, pref))
		if err != nil {
			log.Fatal(err)
		}
		Postfilter(string(e), host, port, key)
	} else {
		e, err := json.Marshal(CreatePolicies(name, pref))
		if err != nil {
			log.Fatal(err)
		}
		Postfilter(string(e), host, port, key)
	}
}

func Postfilter(e, host, port, key string) {
	myurl := url.URL{
		Scheme: "https",
		Host:   host + ":" + port,
		Path:   "/configure",
	}

	data := url.Values{}
	data.Set("data", strings.ToLower(e))
	data.Set("key", key)

	resp, err := http.PostForm(myurl.String(), data)

	if err != nil {
		log.Fatal(err)
	}
	if resp == nil {
		fmt.Println("No response?")
	}

	fmt.Println(resp)
}

func main() {
	Cfg, err := ReadConfig("./config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	mode := strings.ToLower(strings.TrimSpace(Cfg.Mode))
	if mode == "" {
		mode = modeVyOS
	}
	templateDir := Cfg.TemplateDir
	if templateDir == "" {
		templateDir = "templates"
	}

	var renderer *templateRenderer

	getRenderer := func() (*templateRenderer, error) {
		if renderer != nil {
			return renderer, nil
		}
		r, err := newTemplateRenderer(templateDir)
		if err != nil {
			return nil, err
		}
		renderer = r
		return renderer, nil
	}

	for _, peer := range Cfg.Peers {
		peer := peer
		asSet := peer.effectiveASSet()
		if asSet == "" {
			log.Printf("skipping peer %q because no AS-SET or ASN was provided", peer.displayName())
			continue
		}

		fmt.Printf("Requesting prefixes for %s\n", asSet)
		resv4, err := irrdb.Query(Cfg.Irrdb, asSet, "4")
		if err != nil {
			log.Printf("failed to query IPv4 prefixes for %s: %v", asSet, err)
			resv4 = nil
		}
		resv4 = normalizePrefixes(resv4)
		if len(resv4) > 0 {
			for _, pref := range resv4 {
				fmt.Println("accept: " + pref)
			}
		} else {
			fmt.Println("no IPv4 prefixes returned")
		}

		fmt.Println("Building IPv6 filters:")
		resv6, err := irrdb.Query(Cfg.Irrdb, asSet, "6")
		if err != nil {
			log.Printf("failed to query IPv6 prefixes for %s: %v", asSet, err)
			resv6 = nil
		}
		resv6 = normalizePrefixes(resv6)
		if len(resv6) > 0 {
			for _, pref := range resv6 {
				fmt.Println("accept: " + pref)
			}
		} else {
			fmt.Println("no IPv6 prefixes returned")
		}

		if mode == modeVyOS {
			UpdateFilter(asSet+"-v4", resv4, Cfg.Host, Cfg.Port, Cfg.ApiKey)
			UpdateFilter(asSet+"-v6", resv6, Cfg.Host, Cfg.Port, Cfg.ApiKey)
		}

		var limits *peeringDBLimits
		if peer.ASN > 0 {
			limits, err = fetchPeeringDBLimits(Cfg.PeeringDBURL, peer.ASN)
			if err != nil {
				log.Printf("warning: unable to fetch PeeringDB data for AS%d: %v", peer.ASN, err)
			}
		} else {
			log.Printf("warning: ASN missing for %s; skipping PeeringDB prefix limits", peer.displayName())
		}

		vendors := peer.effectiveVendors(Cfg.Vendors, mode)
		if len(vendors) == 0 {
			continue
		}

		renderer, err = getRenderer()
		if err != nil {
			log.Fatalf("unable to initialize template renderer: %v", err)
		}

		for _, vendor := range vendors {
			templateName, ok := supportedVendors[vendor]
			if !ok {
				log.Printf("warning: vendor %q is not supported; skipping", vendor)
				continue
			}
			context := buildTemplateContext(peer, vendor, resv4, resv6, limits)
			rendered, err := renderer.Render(templateName, context)
			if err != nil {
				log.Printf("failed to render template %s for %s: %v", templateName, peer.displayName(), err)
				continue
			}
			if err := writeTemplateOutput(Cfg.OutDir, peer, vendor, rendered); err != nil {
				log.Printf("failed to write rendered template for %s (%s): %v", peer.displayName(), vendor, err)
			}
		}
	}
}
