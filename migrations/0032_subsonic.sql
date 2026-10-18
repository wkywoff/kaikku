-- Пароль для приложений (Subsonic API, internal/api/subsonic.go). Клиенты
-- присылают md5(пароль + соль), поэтому сервер должен знать сам пароль:
-- хранится зашифрованным ключом из JWT_SECRET. NULL — вход из приложений
-- выключен.
ALTER TABLE users ADD COLUMN subsonic_secret BYTEA;
