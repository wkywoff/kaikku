-- Регистрация только по приглашению; первый (самый ранний) пользователь —
-- админ: он создаёт приглашения и может править/удалять каталог.

ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT false;
UPDATE users SET is_admin = true
 WHERE id = (SELECT id FROM users ORDER BY created_at LIMIT 1);

CREATE TABLE invites (
    code       TEXT PRIMARY KEY,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    used_at    TIMESTAMPTZ
);
