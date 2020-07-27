package main

import (
	"encoding/json"
	"fmt"
	"github.com/cryzeck/irrdb"
	"gopkg.in/yaml.v2"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Irrdb  string   `yaml:"irrdb"`
	Host   string   `yaml:"host"`
	Port   string   `yaml:"port"`
	ApiKey string   `yaml:"apikey"`
	Peers  []string `yaml:"peers"`
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
	policies = append(policies, NewPolicy("delete", "policy", "prefix-list").SetValue(name))

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
	for _, peer := range Cfg.Peers {
		fmt.Println("Requesting prefixes for " + peer)
		resv4, err := irrdb.Query(Cfg.Irrdb, peer, "4")
		if err != nil {
			fmt.Println(err)
		} else {
			for _, pref := range resv4 {
				fmt.Println("accept: " + pref)
			}
			UpdateFilter(peer+"-v4", resv4, Cfg.Host, Cfg.Port, Cfg.ApiKey)
		}

		fmt.Println("Building IPv6 filters:")
		resv6, err := irrdb.Query(Cfg.Irrdb, peer, "6")
		if err != nil {
			fmt.Println(err)
		} else {
			for _, pref := range resv6 {
				fmt.Println("accept: " + pref)
			}
			UpdateFilter(peer+"-v6", resv6, Cfg.Host, Cfg.Port, Cfg.ApiKey)
		}
	}
}
