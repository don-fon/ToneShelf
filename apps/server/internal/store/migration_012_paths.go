package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
)

type migrationRootKey struct{}

func init() {
	goose.AddNamedMigrationContext("012_relative_media_paths.go", migrateRelativeMediaPaths, func(context.Context, *sql.Tx) error { return nil })
}

// Version 12 changes path representation only. The transaction also covers the
// goose version record, so a bad legacy path leaves the entire database unchanged.
func migrateRelativeMediaPaths(ctx context.Context, tx *sql.Tx) error {
	root, _ := ctx.Value(migrationRootKey{}).(string)
	if root == "" {
		return nil
	}
	codec := &Store{root: root}
	convert := func(path string) (string, error) {
		if path == "" {
			return "", nil
		}
		if !filepath.IsAbs(path) {
			normalized := strings.ReplaceAll(path, "\\", "/")
			if absolute, err := filepath.Abs(filepath.FromSlash(normalized)); err == nil {
				if relative, err := codec.storedPath(absolute); err == nil {
					return relative, nil
				}
			}
			// Old development records were relative to the repository working directory.
			if index := strings.LastIndex(normalized, "/media/"); index >= 0 {
				prefix := normalized[:index]
				absolute, err := filepath.Abs(filepath.FromSlash(prefix))
				if err != nil {
					return "", err
				}
				if filepath.Clean(absolute) != filepath.Clean(root) && prefix != filepath.Base(root) {
					return "", fmt.Errorf("legacy path is outside data root: %s", path)
				}
				path = normalized[index+1:]
			}
		}
		return codec.storedPath(path)
	}
	for _, spec := range []struct {
		table   string
		columns []string
	}{
		{"tracks", []string{"original_path", "playable_path", "cover_path"}},
		{"upload_jobs", []string{"source_path", "output_path", "pending_candidate"}},
		{"pending_file_deletions", []string{"path"}},
	} {
		rows, err := tx.QueryContext(ctx, "SELECT rowid,"+strings.Join(spec.columns, ",")+" FROM "+spec.table)
		if err != nil {
			return err
		}
		type record struct {
			id     int64
			values []any
		}
		var records []record
		for rows.Next() {
			var id int64
			values := make([]string, len(spec.columns))
			args := []any{&id}
			for i := range values {
				args = append(args, &values[i])
			}
			if err = rows.Scan(args...); err != nil {
				rows.Close()
				return err
			}
			record := record{id: id}
			for i, value := range values {
				if spec.columns[i] == "pending_candidate" && value != "" {
					var candidate NewTrack
					if err = json.Unmarshal([]byte(value), &candidate); err != nil {
						rows.Close()
						return err
					}
					for _, field := range []*string{&candidate.OriginalPath, &candidate.PlayablePath, &candidate.CoverPath} {
						*field, err = convert(*field)
						if err != nil {
							rows.Close()
							return err
						}
					}
					encoded, encodeErr := json.Marshal(candidate)
					if encodeErr != nil {
						rows.Close()
						return encodeErr
					}
					value = string(encoded)
				} else if spec.columns[i] != "pending_candidate" {
					value, err = convert(value)
					if err != nil {
						rows.Close()
						return err
					}
				}
				record.values = append(record.values, value)
			}
			records = append(records, record)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		assignments := make([]string, len(spec.columns))
		for i, column := range spec.columns {
			assignments[i] = column + "=?"
		}
		for _, record := range records {
			args := append(record.values, record.id)
			if _, err = tx.ExecContext(ctx, "UPDATE "+spec.table+" SET "+strings.Join(assignments, ",")+" WHERE rowid=?", args...); err != nil {
				return err
			}
		}
	}
	return nil
}
