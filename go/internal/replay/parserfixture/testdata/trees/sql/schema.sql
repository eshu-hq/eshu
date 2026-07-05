CREATE TABLE accounts (
  id BIGINT PRIMARY KEY,
  email TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL
);

CREATE TABLE invoices (
  id BIGINT PRIMARY KEY,
  account_id BIGINT NOT NULL REFERENCES accounts(id),
  total_cents BIGINT NOT NULL,
  status TEXT NOT NULL
);

CREATE INDEX idx_invoices_account_id ON invoices(account_id);

CREATE VIEW open_invoices AS
SELECT id, account_id, total_cents
FROM invoices
WHERE status = 'open';
