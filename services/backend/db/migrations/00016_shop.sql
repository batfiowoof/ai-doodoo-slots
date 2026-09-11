-- +goose Up
-- The Vault: cosmetics shop. Items are seeded here; ownership lives in
-- user_inventory; the equipped choice per cosmetic slot rides the users row
-- (same pattern as avatar_preset). Everything here is render-only: no shop
-- path ever touches the fair stream or a payout. Purchases move credits
-- through the same ledger discipline as bets (kind 'shop_purchase').
ALTER TABLE users
    ADD COLUMN title         TEXT NOT NULL DEFAULT '',
    ADD COLUMN name_effect   TEXT NOT NULL DEFAULT '',
    ADD COLUMN card_skin     TEXT NOT NULL DEFAULT '',
    ADD COLUMN avatar_frame  TEXT NOT NULL DEFAULT '',
    ADD COLUMN plinko_ball   TEXT NOT NULL DEFAULT '',
    ADD COLUMN profile_theme TEXT NOT NULL DEFAULT '';

CREATE TABLE shop_items (
    id            TEXT PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('title', 'name_effect', 'card_skin',
                                                'avatar_frame', 'plinko_ball',
                                                'profile_theme', 'emote_pack')),
    name          TEXT NOT NULL,
    blurb         TEXT NOT NULL DEFAULT '',
    price_credits BIGINT NOT NULL CHECK (price_credits > 0),
    rarity        TEXT NOT NULL DEFAULT 'common'
                  CHECK (rarity IN ('common', 'rare', 'epic', 'legendary')),
    -- Kind-specific extras; emote packs carry their unlock list here.
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    sort          INT NOT NULL DEFAULT 0,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE user_inventory (
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id     TEXT NOT NULL REFERENCES shop_items(id),
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, item_id)
);

INSERT INTO shop_items (id, kind, name, blurb, price_credits, rarity, payload, sort) VALUES
    -- titles
    ('title.rookie',      'title', 'ROOKIE',      'Everyone starts somewhere.',                 500,    'common',    '{}', 1),
    ('title.degen',       'title', 'DEGEN',       'Chase the loss. Chase the win. Repeat.',     2500,   'common',    '{}', 2),
    ('title.card-shark',  'title', 'CARD SHARK',  'The table watches your hands.',              5000,   'rare',      '{}', 3),
    ('title.high-roller', 'title', 'HIGH ROLLER', 'Valet is free at your bet size.',            10000,  'rare',      '{}', 4),
    ('title.vip',         'title', 'VIP',         'Skip the queue. All of them.',               25000,  'epic',      '{}', 5),
    ('title.whale',       'title', 'WHALE',       'The felt bends to your stack.',              50000,  'epic',      '{}', 6),
    ('title.legend',      'title', 'LEGEND',      'They will tell stories about this name.',    100000, 'legendary', '{}', 7),
    -- name effects
    ('name.rainbow', 'name_effect', 'RAINBOW FLOW',  'A slow cycle through every color the cabinet has.', 5000,  'rare',      '{}', 1),
    ('name.neon',    'name_effect', 'NEON FLICKER',  'Buzzing sign energy. Occasionally blinks out.',     7500,  'rare',      '{}', 2),
    ('name.ice',     'name_effect', 'CRYO',          'Frozen solid. Chips come out cold.',                10000, 'rare',      '{}', 3),
    ('name.fire',    'name_effect', 'INFERNO',       'Burning through stacks since forever.',             10000, 'rare',      '{}', 4),
    ('name.gold',    'name_effect', 'MIDAS',         'Everything you touch turns to credits. Legally.',   15000, 'epic',      '{}', 5),
    ('name.glitch',  'name_effect', 'GLITCH',        'Reality buffer underflow.',                         50000, 'legendary', '{}', 6),
    -- card skins
    ('card.midnight',  'card_skin', 'MIDNIGHT',  'Ink-black backs, silver pips.',            2500,  'rare',      '{}', 1),
    ('card.synthwave', 'card_skin', 'SYNTHWAVE', 'Sunset gradient backs. 1984 forever.',     5000,  'epic',      '{}', 2),
    ('card.8bit',      'card_skin', '8-BIT',     'Chunky pixels, honest pips.',              7500,  'rare',      '{}', 3),
    ('card.crimson',   'card_skin', 'CRIMSON',   'The house edge wears red.',                12500, 'epic',      '{}', 4),
    ('card.gold-foil', 'card_skin', 'GOLD FOIL', 'Stamped, polished, insufferable.',         20000, 'legendary', '{}', 5),
    -- avatar frames
    ('frame.bronze',  'avatar_frame', 'BRONZE RING', 'Solid. Unassuming. Yours.',        1000,  'common', '{}', 1),
    ('frame.silver',  'avatar_frame', 'SILVER RING', 'A step up the medal ladder.',      2500,  'common', '{}', 2),
    ('frame.neon',    'avatar_frame', 'NEON HALO',   'Your avatar, backlit by the strip.', 5000, 'rare',   '{}', 3),
    ('frame.gold',    'avatar_frame', 'GOLD RING',   'Heavy is the head.',               7500,  'rare',   '{}', 4),
    ('frame.rainbow', 'avatar_frame', 'PRISM RING',  'An orbit of every color at once.', 25000, 'epic',   '{}', 5),
    -- plinko balls
    ('ball.chrome',  'plinko_ball', 'CHROME',     'Factory finish.',                1500,  'common',    '{}', 1),
    ('ball.neon',    'plinko_ball', 'NEON PUCK',  'Glows on the way down.',         3000,  'common',    '{}', 2),
    ('ball.gold',    'plinko_ball', 'GOLD PUCK',  'Drops like it costs something.', 5000,  'rare',      '{}', 3),
    ('ball.rainbow', 'plinko_ball', 'PRISM PUCK', 'Leaves a rumor of a trail.',     10000, 'epic',      '{}', 4),
    ('ball.plasma',  'plinko_ball', 'PLASMA',     'Not entirely legal in most jurisdictions.', 25000, 'legendary', '{}', 5),
    -- profile themes
    ('theme.midnight',  'profile_theme', 'MIDNIGHT',    'Deep space behind your stats.', 2500,  'common', '{}', 1),
    ('theme.felt',      'profile_theme', 'CASINO FELT', 'You are the table now.',        2500,  'common', '{}', 2),
    ('theme.synthwave', 'profile_theme', 'SYNTHWAVE',   'Chrome sunset profile card.',   5000,  'rare',   '{}', 3),
    ('theme.gold',      'profile_theme', 'GILDED',      'For profiles that cost a lot.', 10000, 'epic',   '{}', 4),
    -- emote packs (payload lists the emote ids the pack unlocks)
    ('pack.party',      'emote_pack', 'PARTY PACK',      'Five ways to celebrate loudly.',      2500,  'rare', '{"emotes":["party","bolt","gem","rocket","disco"]}', 1),
    ('pack.cope',       'emote_pack', 'COPE PACK',       'For the river that busted you.',      2500,  'rare', '{"emotes":["tilt","sweat","ghost","alien","poop"]}', 2),
    ('pack.highroller', 'emote_pack', 'HIGH-ROLLER PACK','Crown-tier reactions only.',          10000, 'epic', '{"emotes":["whale","crown","trophy","moneybag","genie"]}', 3)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE user_inventory;
DROP TABLE shop_items;
ALTER TABLE users
    DROP COLUMN IF EXISTS profile_theme,
    DROP COLUMN IF EXISTS plinko_ball,
    DROP COLUMN IF EXISTS avatar_frame,
    DROP COLUMN IF EXISTS card_skin,
    DROP COLUMN IF EXISTS name_effect,
    DROP COLUMN IF EXISTS title;
