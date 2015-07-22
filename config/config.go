package config

import (
	"encoding/json"
	"io/ioutil"
)


var config map[string]string

func init() {
	configBytes, err := ioutil.ReadFile("config.json")
	if err != nil {
		panic("Unable to read config.json: " + err.Error())
	}

	if err = json.Unmarshal(configBytes, &config); err != nil {
		panic("Unable to parse config.json: " + err.Error())
	}
}

func Get(key string) string {
	return config[key]
}
