-- +goose Up
-- Personalization foundation: catalog metadata, a lightweight launch-event
-- sink (covers guests who browse but never bet), and the per-player lobby
-- preference. Only the api process reads or writes these tables, so the
-- gameserver's migration-less boot is unaffected.

CREATE TABLE game_metadata (
    game_id      TEXT PRIMARY KEY,
    category     TEXT NOT NULL CHECK (category IN ('slots', 'instant', 'table', 'live')),
    collection   TEXT NOT NULL DEFAULT '',
    tags         TEXT[] NOT NULL DEFAULT '{}',
    is_new_until TIMESTAMPTZ,
    blurb        TEXT NOT NULL DEFAULT ''
);

INSERT INTO game_metadata (game_id, category, collection, tags, is_new_until, blurb) VALUES
    ('slots',    'slots',   'classic',   ARRAY['reels','classic','retro'],       NULL, 'Three reels, one line, pure nerve.'),
    ('fruits',   'slots',   'fruit',     ARRAY['reels','fruit','casual'],        NULL, 'Cherries, clovers and honest lemons.'),
    ('treasure', 'slots',   'adventure', ARRAY['reels','adventure','scatter'],   now() + interval '30 days', 'X marks the payline.'),
    ('dice',     'instant', 'originals', ARRAY['multiplier','risk','fast'],      NULL, 'Over or under — you set the odds.'),
    ('plinko',   'instant', 'originals', ARRAY['physics','casual','multiplier'], NULL, 'Drop the puck, hold your breath.'),
    ('mines',    'instant', 'originals', ARRAY['grid','risk','cashout'],         NULL, 'Five by five — don''t hit one.'),
    ('blackjack','table',   'originals', ARRAY['cards','skill','classic'],       NULL, 'Beat the dealer to twenty-one.'),
    ('crash',    'live',    'originals', ARRAY['multiplier','cashout','social'], NULL, 'Cash out before the ship burns.'),
    ('roulette', 'live',    'originals', ARRAY['wheel','table','social'],        NULL, 'European wheel, single zero.'),
    ('holdem',   'live',    'originals', ARRAY['cards','poker','social'],        NULL, 'No-limit Texas hold''em.')
ON CONFLICT (game_id) DO NOTHING;

CREATE TABLE player_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    session_id BIGINT REFERENCES sessions(id),
    event_type TEXT NOT NULL CHECK (event_type IN ('launch')),
    game_id    TEXT NOT NULL,
    context    JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trending scans by game; per-player affinity scans by user+time.
CREATE INDEX idx_player_events_trending ON player_events (game_id, created_at DESC) WHERE event_type = 'launch';
CREATE INDEX idx_player_events_user ON player_events (user_id, created_at DESC);

CREATE TABLE user_prefs (
    user_id             BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    personalize_enabled BOOLEAN NOT NULL DEFAULT true,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE user_prefs;
DROP TABLE player_events;
DROP TABLE game_metadata;
