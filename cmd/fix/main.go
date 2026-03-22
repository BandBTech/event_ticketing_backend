package main

import (
	"fmt"
	"log"

	"event-ticketing-backend/internal/database"
	"event-ticketing-backend/internal/models"
	"event-ticketing-backend/pkg/config"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	if err := database.Connect(cfg); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	fmt.Println("Starting event capacity/available fix...")

	// Get all events with tiers
	var events []models.Event
	if err := database.DB.Preload("Tiers").Where("deleted_at IS NULL").Find(&events).Error; err != nil {
		log.Fatalf("Failed to fetch events: %v", err)
	}

	fixedCount := 0
	for _, event := range events {
		if len(event.Tiers) == 0 {
			continue // Skip events without tiers
		}

		// Calculate total capacity and available from tiers
		totalCapacity := 0
		totalAvailable := 0
		for _, tier := range event.Tiers {
			totalCapacity += tier.Quantity
			totalAvailable += tier.Available
		}

		// Update event if values differ
		if event.Capacity != totalCapacity || event.Available != totalAvailable {
			if err := database.DB.Model(&event).Updates(map[string]interface{}{
				"capacity":  totalCapacity,
				"available": totalAvailable,
			}).Error; err != nil {
				log.Printf("Failed to update event %s: %v", event.ID, err)
				continue
			}
			fmt.Printf("Fixed event %s: capacity %d->%d, available %d->%d\n",
				event.ID, event.Capacity, totalCapacity, event.Available, totalAvailable)
			fixedCount++
		}
	}

	fmt.Printf("Fixed %d events\n", fixedCount)
	fmt.Println("Event capacity/available fix completed.")
}
