package tools

import (
	"database/sql"
	"embed"
	"io/fs"
	"log"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migration/*
var migrationFiles embed.FS

func ConnectSqlite(filePath string) (*sql.DB, error) {
	db, err := connectWithBackoff("sqlite3", filePath, 3)
	if err != nil {
		return nil, err
	}

	err = RunMigrations(db)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func RunMigrations(db *sql.DB) error {
	dirEntries, err := fs.ReadDir(migrationFiles, "migration")
	if err != nil {
		return err
	}
	for _, entry := range dirEntries {
		fileName := filepath.Join("migration", entry.Name())
		fileData, err := fs.ReadFile(migrationFiles, fileName)
		if err != nil {
			return err
		}
		if _, err := db.Exec(string(fileData)); err != nil {
			return err
		}
	}

	return nil
}

func connectWithBackoff(driver string, connStr string, maxRetries int) (*sql.DB, error) {
	var db *sql.DB
	var err error
	for i := 0; i < maxRetries; i++ {
		db, err = sql.Open(driver, connStr)
		if err != nil {
			log.Println("Failed attempt to connect to " + driver + ": " + err.Error())
			time.Sleep(time.Duration(i+1) * (3 * time.Second))
			continue
		}
		err = db.Ping()
		if err != nil {
			log.Println("Failed attempt to connect to " + driver + ": " + err.Error())
			time.Sleep(time.Duration(i+1) * (3 * time.Second))
			continue
		}
		return db, nil
	}
	return nil, err
}
