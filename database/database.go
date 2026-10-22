package database

import (
	"fmt"
	"log"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB
var OnConnect []func(*gorm.DB)

func Init() {
	go connect()
}

func connect() {
	cfg := config.AppConfig.Database
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.DBName, cfg.Charset)

	for {
		retryCount := 0
		for {
			var err error
			DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
			if err == nil {
				log.Println("Database connection established")
				for _, fn := range OnConnect {
					go fn(DB)
				}
				return
			}

			log.Printf("Failed to connect to database: %v. Retrying in %d seconds...", err, cfg.RetryIntervalSec)
			retryCount++

			if cfg.MaxRetries > 0 && retryCount >= cfg.MaxRetries {
				log.Printf("Reached maximum retry attempts (%d).", cfg.MaxRetries)
				break
			}

			time.Sleep(time.Duration(cfg.RetryIntervalSec) * time.Second)
		}

		if cfg.NextRoundDelayMin <= 0 {
			log.Println("Database connection failed and next round delay is 0. Stopping retries.")
			return
		}

		log.Printf("Starting next round of retries in %d minutes...", cfg.NextRoundDelayMin)
		time.Sleep(time.Duration(cfg.NextRoundDelayMin) * time.Minute)
	}
}

func RegisterOnConnect(fn func(*gorm.DB)) {
	if DB != nil {
		go fn(DB)
	} else {
		OnConnect = append(OnConnect, fn)
	}
}
