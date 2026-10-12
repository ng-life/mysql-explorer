package mysql

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

// Catalog is published only after all metadata queries finish successfully.
// It includes only schemas visible to the configured account, and no table data.
type Catalog struct {
	Databases []string                       `json:"databases"`
	Schemas   map[string]map[string][]Column `json:"schemas"`
	LoadedAt  time.Time                      `json:"loaded_at"`
}

func (c *Client) Metadata() *Catalog {
	c.catalogMu.RLock()
	defer c.catalogMu.RUnlock()
	return c.catalog
}

// Refresh reads metadata in bulk rather than opening one query per table.
// Readers retain the previous complete snapshot until the new one is ready.
func (c *Client) Refresh(ctx context.Context) (*Catalog, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	names, err := list(ctx, c.DB, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	out := &Catalog{Databases: names, Schemas: map[string]map[string][]Column{}}
	for _, name := range names {
		out.Schemas[name] = map[string][]Column{}
	}
	tables, err := c.DB.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME FROM information_schema.TABLES ORDER BY TABLE_SCHEMA, TABLE_NAME")
	if err != nil {
		return nil, err
	}
	for tables.Next() {
		var db, table string
		if err := tables.Scan(&db, &table); err != nil {
			tables.Close()
			return nil, err
		}
		if schema, ok := out.Schemas[db]; ok {
			schema[table] = []Column{}
		}
	}
	err = tables.Err()
	tables.Close()
	if err != nil {
		return nil, err
	}
	columns, err := c.DB.QueryContext(ctx, "SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION")
	if err != nil {
		return nil, err
	}
	for columns.Next() {
		var db, table, nullable string
		var col Column
		if err := columns.Scan(&db, &table, &col.Name, &col.Type, &nullable, &col.Key, &col.Comment); err != nil {
			columns.Close()
			return nil, err
		}
		col.Nullable = nullable == "YES"
		if schema, ok := out.Schemas[db]; ok {
			if _, exists := schema[table]; exists {
				schema[table] = append(schema[table], col)
			}
		}
	}
	err = columns.Err()
	columns.Close()
	if err != nil {
		return nil, err
	}
	out.LoadedAt = time.Now().UTC()
	c.catalogMu.Lock()
	c.catalog = out
	c.catalogMu.Unlock()
	return out, nil
}

// Preload isolates failures by service and waits for all startup attempts.
func (p *Pool) Preload(ctx context.Context) {
	var wg sync.WaitGroup
	for id, cluster := range p.clusters {
		wg.Add(1)
		go func(id int, name string) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
			defer cancel()
			reader, err := p.Get(bounded, id)
			var catalog *Catalog
			if err == nil {
				catalog, err = reader.Refresh(bounded)
			}
			if err != nil {
				log.Printf("Metadata preload failed for service %q; check connection and permissions", name)
				return
			}
			tables, columns := 0, 0
			for _, schema := range catalog.Schemas {
				tables += len(schema)
				for _, fields := range schema {
					columns += len(fields)
				}
			}
			log.Printf("Metadata loaded for service %q: %d databases, %d tables, %d columns", name, len(catalog.Databases), tables, columns)
		}(id, cluster.Name)
	}
	wg.Wait()
}

// copyCatalog copies mutable maps so in-flight HTTP readers see a stable snapshot.
func copyCatalog(old *Catalog) *Catalog {
	if old == nil {
		return nil
	}
	out := &Catalog{Databases: append([]string{}, old.Databases...), Schemas: map[string]map[string][]Column{}, LoadedAt: time.Now().UTC()}
	for db, schema := range old.Schemas {
		out.Schemas[db] = map[string][]Column{}
		for table, cols := range schema {
			out.Schemas[db][table] = cols
		}
	}
	return out
}
func (c *Client) RefreshTables(ctx context.Context, db string) ([]string, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	names, err := list(ctx, c.DB, "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME", db)
	if err != nil {
		return nil, err
	}
	old := c.Metadata()
	if old == nil {
		return names, nil
	}
	out := copyCatalog(old)
	schema := map[string][]Column{}
	for _, table := range names {
		cols := old.Schemas[db][table]
		if cols == nil {
			cols = []Column{}
		}
		schema[table] = cols
	}
	if _, exists := out.Schemas[db]; !exists {
		out.Databases = append(out.Databases, db)
		sort.Strings(out.Databases)
	}
	out.Schemas[db] = schema
	c.catalogMu.Lock()
	c.catalog = out
	c.catalogMu.Unlock()
	return names, nil
}
func (c *Client) RefreshColumns(ctx context.Context, db, table string) ([]Column, error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	cols, err := c.liveColumns(ctx, db, table)
	if err != nil {
		return nil, err
	}
	old := c.Metadata()
	if old == nil {
		return cols, nil
	}
	out := copyCatalog(old)
	if _, exists := out.Schemas[db]; !exists {
		out.Schemas[db] = map[string][]Column{}
		out.Databases = append(out.Databases, db)
		sort.Strings(out.Databases)
	}
	if len(cols) == 0 {
		delete(out.Schemas[db], table)
	} else {
		out.Schemas[db][table] = cols
	}
	c.catalogMu.Lock()
	c.catalog = out
	c.catalogMu.Unlock()
	return cols, nil
}
