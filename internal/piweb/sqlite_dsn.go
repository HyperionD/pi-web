package piweb

import "net/url"

// sqliteDSN builds a SQLite DSN that applies pragmas to every pooled
// connection. Pragmas executed through *sql.DB.Exec only configure the single
// connection the pool happens to hand out; the remaining connections keep the
// driver defaults (busy_timeout=0) and turn concurrent writers into immediate
// SQLITE_BUSY failures instead of waiting for the write lock.
func sqliteDSN(path string, pragmas ...string) string {
	target := url.URL{Scheme: "file", Path: path}
	params := url.Values{}
	for _, pragma := range pragmas {
		params.Add("_pragma", pragma)
	}
	target.RawQuery = params.Encode()
	return target.String()
}

var eventStorePragmas = []string{
	"busy_timeout(5000)",
	"synchronous(FULL)",
	"foreign_keys(ON)",
	"journal_mode(WAL)",
	"wal_autocheckpoint(1000)",
}

var webDBPragmas = []string{
	"busy_timeout(5000)",
	"foreign_keys(ON)",
	"journal_mode(WAL)",
}
