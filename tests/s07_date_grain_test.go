package tests

import "testing"

// §7 — Date / time expressions and dialect splits

// §7 CAN TEST NOW: EXTRACT(DOW) renders per-dialect
func TestExtractDow(t *testing.T) {
	pgSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_extract_dow.json", "postgres")
	assertContains(t, pgSQL, "EXTRACT(DOW")

	bqSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_extract_dow.json", "bigquery")
	assertContains(t, bqSQL, "EXTRACT(DAYOFWEEK")
}

// §7 CAN TEST NOW: Date arithmetic expressions render per-dialect
func TestDateArithmetic(t *testing.T) {
	pgSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_date_arithmetic.json", "postgres")
	assertContains(t, pgSQL, "INTERVAL")

	bqSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_date_arithmetic.json", "bigquery")
	assertContains(t, bqSQL, "DATE_ADD")
}

// §7 CAN TEST NOW: AT TIME ZONE / DATETIME timezone forms render per-dialect
func TestTimezone(t *testing.T) {
	pgSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_timezone.json", "postgres")
	assertContains(t, pgSQL, "AT TIME ZONE")

	bqSQL := compileSQLDialect(t, "ecommerce.yaml", "s07_timezone.json", "bigquery")
	assertContains(t, bqSQL, "DATETIME(")
}
