package main

import (
    "fmt"
	"log"
	"os"
	"bufio"
	"strings"
	"net/url"
	"net/http"
	"gopkg.in/yaml.v2"
)
type Config struct {
	Host 	string	`yaml:"host"`
	Port    string	`yaml:"port"`
	Path	string	`yaml:"path"`
	ApiKey	string	`yaml:"apikey"`
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

func CreatePolicies(input []string) []string {
	var policies []string
	for i := 0; i < len(input); i++ {
		policies = append(policies,
			`"{"op": "set", 
			   "path":["policy", "prefix-list6", "test-list", "rule", " + i + 1 + ", "prefix"],
			   "value": "` + input[i] + `"
			   }`)
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
	
	// Printing config for testing
	fmt.Println(cfg)
	var joined string;

	lines := ReadInput()
  	policies := CreatePolicies(lines)
	joined = strings.Join(policies, ",")
	joined = "[" + joined + "]"

  	url := url.URL {
		Scheme: "https",
		Host: cfg.Host+":"+cfg.Port,
		Path: cfg.Path,
	}
	fmt.Println(url.String())
	
  	resp, err := http.PostForm(url.String(), url.Values {
		"data": joined,
		"key": cfg.ApiKey,
	})

	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(resp)
}
