-- Two user types: administrators can reach Settings, users get every other page.
-- The account created during first-run setup becomes the administrator.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';

UPDATE users SET role = 'admin' WHERE id = (SELECT MIN(id) FROM users);
