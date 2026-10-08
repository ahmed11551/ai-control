-- Персональное название чата (логист переименовывает у себя)
CREATE TABLE IF NOT EXISTS staff_dialog_titles (
    dialog_id UUID NOT NULL REFERENCES staff_dialogs(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (dialog_id, user_id)
);
