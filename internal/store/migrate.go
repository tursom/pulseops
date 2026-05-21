package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const schemaVersionKey = "schema_version"

func runMigrations(db *sql.DB) error {
	currentVersion, err := readSchemaVersion(db)
	if err != nil {
		return err
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", m.name, err)
		}

		if _, err := db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("run migration %03d (%s): %w", m.version, m.name, err)
		}

		if err := setSchemaVersion(db, m.version); err != nil {
			return err
		}
	}

	return nil
}

func readSchemaVersion(db *sql.DB) (int, error) {
	var versionStr string
	err := db.QueryRow(
		`SELECT value FROM kv_metadata WHERE key = $1`, schemaVersionKey,
	).Scan(&versionStr)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}

	v, err := strconv.Atoi(versionStr)
	if err != nil {
		return 0, fmt.Errorf("parse schema_version %q: %w", versionStr, err)
	}
	return v, nil
}

type migration struct {
	version int
	name    string
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(e.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		migrations = append(migrations, migration{version: v, name: e.Name()})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return migrations, nil
}

func setSchemaVersion(db *sql.DB, version int) error {
	_, err := db.Exec(
		`INSERT INTO kv_metadata (key, value, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		schemaVersionKey, strconv.Itoa(version),
	)
	if err != nil {
		return fmt.Errorf("update schema_version to %d: %w", version, err)
	}
	return nil
}
