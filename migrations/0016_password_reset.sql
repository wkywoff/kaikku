-- Смена и сброс пароля (handlers_password.go).
--
-- password_changed_at: токены, выданные раньше, больше не пускают (router.go,
-- authed) — после смены или сброса пароля остальные устройства выходят.
ALTER TABLE users ADD COLUMN password_changed_at TIMESTAMPTZ;

-- Одноразовая ссылка на новый пароль, которую админ создаёт для человека
-- (почты у пользователей нет). В базе — только sha256 кода: утечка базы не
-- даёт войти по ещё живой ссылке.
CREATE TABLE password_resets (
    code_hash  TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX idx_password_resets_user ON password_resets (user_id);
