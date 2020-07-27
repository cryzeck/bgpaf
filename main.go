package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Host   string `yaml:"host"`
	Port   string `yaml:"port"`
	Path   string `yaml:"path"`
	ApiKey string `yaml:"apikey"`
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

func CreatePolicies(input []string) []*policy {
	policies := make([]*policy, 0, len(input)*2+1)
	policies = append(policies, NewPolicy("delete", "policy", "prefix-list").SetValue("test-list"))

	rule := NewPolicy("set", "policy", "prefix-list", "test-list", "rule")
	for i, value := range input {
		key := strconv.Itoa(i + 1)

		policies = append(policies,
			rule.CloneExtend(key, "action").SetValue("permit"),
			rule.CloneExtend(key, "prefix").SetValue(value),
		)
	}

	return policies
}

func ReadInput() []string {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return lines
}

func main() {
	cfg, err := ReadConfig("./config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	lines := ReadInput()
	e, err := json.Marshal(CreatePolicies(lines))
	if err != nil {
		log.Fatal(err)
	}

	myurl := url.URL{
		Scheme: "https",
		Host:   cfg.Host + ":" + cfg.Port,
		Path:   cfg.Path,
	}

	data := url.Values{}
	data.Set("data", strings.ToLower(string(e)))
	data.Set("key", cfg.ApiKey)

	fmt.Println(myurl.String())

	resp, err := http.PostForm(myurl.String(), data)

	fmt.Println(data)

	if err != nil {
		log.Fatal(err)
	}
	if resp == nil {
		fmt.Println("No response?")
	}

	fmt.Println(resp)
}
