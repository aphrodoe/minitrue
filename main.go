package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/minitrue/internal/models"
	"github.com/minitrue/internal/storage"
)

func main() {
	//hardcoded input for now, will be replaced by IoT ingestion system later..
	jsonInput := `[
		{"timestamp": 1609459200, "value": 23.5},
		{"timestamp": 1609459260, "value": 23.7},
		{"timestamp": 1609459320, "value": 23.6},
		{"timestamp": 1609459380, "value": 24.1},
		{"timestamp": 1609459440, "value": 24.3},
		{"timestamp": 1609459500, "value": 24.2},
		{"timestamp": 1609459560, "value": 24.5},
		{"timestamp": 1609459620, "value": 24.8},
		{"timestamp": 1609459680, "value": 25.1},
		{"timestamp": 1609459740, "value": 25.0}
	]`

	var records []models.Record
	if err := json.Unmarshal([]byte(jsonInput), &records); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	fmt.Printf("Parsed %d records from JSON input\n", len(records))

	engine := storage.NewStorageEngine("data.parq")
	
	if err := engine.Write(records); err != nil {
		log.Fatalf("Failed to write data: %v", err)
	}

	fmt.Println("Successfully wrote compressed data to data.parq")

	readRecords, err := engine.Read()
	if err != nil {
		log.Fatalf("Failed to read data: %v", err)
	}

	fmt.Printf("Successfully read %d records from data.parq\n", len(readRecords))
	fmt.Println("\nFirst 3 records:")
	for i := 0; i < 3 && i < len(readRecords); i++ {
		fmt.Printf("  Timestamp: %d, Value: %.2f\n", readRecords[i].Timestamp, readRecords[i].Value)
	}
}

