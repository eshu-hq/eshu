// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"sort"
)

// ddlParityGaps compares a Go DDL inventory with the numbered-migration
// inventory and returns one sorted message per violation:
//
//   - a column, named constraint, or trigger in Go DDL on a migration-created
//     table that no migration declares, or on a table nothing creates;
//   - an index name no migration creates, or one a migration puts on another
//     table;
//   - a function name no migration creates;
//   - a table Go DDL creates that no migration creates, unless goOnlyTables
//     lists it;
//   - a statement the scan cannot read, unless dynamicDDL lists its
//     ddlUnresolved.key;
//   - an allowlist entry in either map that matched nothing, so exemptions
//     cannot outlive the code they exempt.
func ddlParityGaps(goInv, migInv ddlInventory, goOnlyTables, dynamicDDL map[string]string) []string {
	var gaps []string
	add := func(format string, args ...any) { gaps = append(gaps, fmt.Sprintf(format, args...)) }
	usedTables := map[string]bool{}
	usedDynamic := map[string]bool{}

	// goOwned reports whether Go DDL itself creates table, so a column or
	// object on it is judged by the table rule instead.
	goOwned := func(table string, site ddlSite) bool {
		_, created := goInv.tables[table]
		_, temp := goInv.tempTables[ddlTempTable{file: site.file, table: table}]
		return created || temp
	}
	tableKnown := func(table string, site ddlSite) (inMigrations, skip bool) {
		_, inMigrations = migInv.tables[table]
		return inMigrations, !inMigrations && goOwned(table, site)
	}

	for table, site := range goInv.tables {
		if _, ok := migInv.tables[table]; ok {
			continue
		}
		if _, ok := goOnlyTables[table]; ok {
			usedTables[table] = true
			continue
		}
		add("%s: table %s is created by Go DDL but by no numbered migration", site, table)
	}
	for col, site := range goInv.columns {
		switch inMigrations, skip := tableKnown(col.table, site); {
		case skip:
		case !inMigrations:
			add("%s: column %s.%s is added by Go DDL to a table no numbered migration creates", site, col.table, col.column)
		default:
			if _, ok := migInv.columns[col]; !ok {
				add("%s: column %s.%s exists in Go DDL but in no numbered migration", site, col.table, col.column)
			}
		}
	}
	for name, idx := range goInv.indexes {
		mig, inMigrations := migInv.indexes[name]
		switch _, skip := tableKnown(idx.table, idx.site); {
		case skip:
		case !inMigrations:
			add("%s: index %s on %s exists in Go DDL but in no numbered migration", idx.site, name, idx.table)
		case mig.table != idx.table:
			add("%s: index %s is on %s in Go DDL but on %s in the numbered migrations", idx.site, name, idx.table, mig.table)
		}
	}
	for obj, site := range goInv.objects {
		if obj.table != "" {
			inMigrations, skip := tableKnown(obj.table, site)
			if skip {
				continue
			}
			if !inMigrations {
				add("%s: %s %s is created by Go DDL on %s, a table no numbered migration creates", site, obj.kind, obj.name, obj.table)
				continue
			}
		}
		if _, ok := migInv.objects[obj]; !ok {
			add("%s: %s %s%s exists in Go DDL but in no numbered migration", site, obj.kind, obj.name, onTable(obj.table))
		}
	}
	for _, u := range goInv.unresolved {
		if _, ok := dynamicDDL[u.key()]; ok {
			usedDynamic[u.key()] = true
			continue
		}
		add("%s: unresolved DDL in %q; use literal SQL or add %q to ddlParityDynamicDDL with a reason", u.site, u.head, u.key())
	}
	for table := range goOnlyTables {
		if !usedTables[table] {
			add("ddlParityGoOnlyTables entry %q matched no Go-only table; remove it", table)
		}
	}
	for key := range dynamicDDL {
		if !usedDynamic[key] {
			add("ddlParityDynamicDDL entry %q matched no unresolved Go DDL; remove it", key)
		}
	}
	sort.Strings(gaps)
	return gaps
}

// onTable formats " on <table>" for a table-scoped object.
func onTable(table string) string {
	if table == "" {
		return ""
	}
	return " on " + table
}
