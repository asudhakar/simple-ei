package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Configuration struct {
	ApplicationName       string
	Port                  string
	BaseURL               string
	PostalCodeEndpoint    string
	PostalCodePageTableID string
	EIPageTableID         string
}

func Load() (Configuration, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Println("Error getting currentDir:", err)
		return Configuration{}, err
	}
	file, err := os.Open(currentDir + "/config/config.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return Configuration{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		fmt.Println("error:", err)
	}
	return configuration, nil
}
