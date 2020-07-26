package main

import (
  "fmt"
  "log"
  "os"
  "bufio"
  "encoding/json"
  "net/url"
  "net/http"
  "strings"
  "gopkg.in/yaml.v2"
)

type Config struct {
  Host    string  `yaml:"host"`
  Port    string  `yaml:"port"`
	Path    string  `yaml:"path"`
	ApiKey  string  `yaml:"apikey"`
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
  Op      string
  Path    []string
  Value   string
}

func CreatePolicies(input []string) []*policy {
  policies := []*policy{}
  del := new(policy)
  del.Op = "delete"
  del.Path = []string{"policy", "prefix-list"}
  del.Value = "test-list"
  policies = append(policies, del)

  for i := 0; i < len(input); i++ {
    pol := new(policy)
    per := new(policy)
    pol.Op = "set"
    pol.Path = []string{"policy", "prefix-list", "test-list", "rule", fmt.Sprintf("%d", i+1), "action"}
    pol.Value = "permit"
    policies = append(policies, pol)
    per.Op = "set"
    per.Path = []string{"policy", "prefix-list", "test-list", "rule", fmt.Sprintf("%d", i+1), "prefix"}
    per.Value = input[i]
    policies = append(policies, per)
  }
  return policies
}

func ReadInput() []string{
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

  myurl := url.URL {
    Scheme: "https",
    Host: cfg.Host+":"+cfg.Port,
    Path: cfg.Path,
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
