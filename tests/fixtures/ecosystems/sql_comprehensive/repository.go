package sqlfixture

import "database/sql"

func listUsers(db *sql.DB) error {
	_, err := db.Exec("SELECT id FROM public.users")
	return err
}
