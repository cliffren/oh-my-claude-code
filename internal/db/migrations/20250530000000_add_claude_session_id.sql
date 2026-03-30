-- +goose Up
-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN claude_session_id TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN claude_session_id;
-- +goose StatementEnd
