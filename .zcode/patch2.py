import io, glob

p = "apps/web/app/page.tsx"
s = io.open(p, encoding="utf-8").read()

# header: account/deposit/verify/staff cluster around SND toggle
old = '''          <button
            type="button"
            onClick={toggleMute}
            style={{
              border: "1px solid #6b4a1c",
              background: "#2a1406",
              color: "#ffb15c",
              fontFamily: "var(--font-display)",
              fontSize: 12,
              letterSpacing: "1px",
              padding: "10px 14px",
              whiteSpace: "nowrap",
              cursor: "pointer",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.borderColor = "#ff8a1f";
              e.currentTarget.style.boxShadow = "0 0 14px rgba(255,138,31,.4)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.borderColor = "#6b4a1c";
              e.currentTarget.style.boxShadow = "none";
            }}
          >
            {muted ? "SND OFF" : "SND ON"}
          </button>
        </header>'''
new = '''          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            {session.data && (
              <button
                type="button"
                onClick={() => {
                  sound.unlock();
                  sound.click();
                  setAccountOpen(true);
                }}
                title={session.data.user.email ?? session.data.user.displayName}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  border: "1px solid #4a3a72",
                  background: "#1d1036",
                  padding: "5px 10px",
                  cursor: "pointer",
                  maxWidth: 220,
                }}
                onMouseEnter={(e) => {
                  e.currentTarget.style.borderColor = "#22e8ff";
                  e.currentTarget.style.boxShadow = "0 0 14px rgba(34,232,255,.4)";
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.borderColor = "#4a3a72";
                  e.currentTarget.style.boxShadow = "none";
                }}
              >
                <Avatar
                  userId={session.data.user.id}
                  displayName={session.data.user.displayName}
                  avatarPreset={session.data.user.avatarPreset}
                  avatarVersion={session.data.user.avatarVersion}
                  size={22}
                  ring="#22e8ff"
                />
                <span
                  style={{
                    fontFamily: "var(--font-display)",
                    fontSize: 11,
                    letterSpacing: 1,
                    color: "#cfc4f2",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  {session.data.user.displayName}
                </span>
              </button>
            )}
            <button
              type="button"
              onClick={doDeposit}
              disabled={deposit.isPending || !session.isSuccess}
              style={{
                border: "2px solid #ff8a1f",
                background: "#2a1406",
                color: "#ff8a1f",
                fontFamily: "var(--font-display)",
                fontSize: 11,
                letterSpacing: 1,
                padding: "8px 12px",
                whiteSpace: "nowrap",
                cursor: deposit.isPending ? "wait" : "pointer",
                opacity: deposit.isPending ? 0.6 : 1,
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = "#ff8a1f";
                e.currentTarget.style.color = "#06040d";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = "#2a1406";
                e.currentTarget.style.color = "#ff8a1f";
              }}
            >
              {deposit.isPending ? "\u2026" : "+1000"}
            </button>
            <NavLink href="/verify">VERIFY</NavLink>
            {session.data && !session.data.user.isGuest && (
              <NavLink href="/auth/logout" hard>
                LOGOUT
              </NavLink>
            )}
            {!session.data && (
              <NavLink href="/auth/login?next=/" hard>
                LOGIN
              </NavLink>
            )}
            {session.data && (session.data.user.role === "admin" || session.data.user.role === "moderator") && (
              <NavLink href="/admin">
                STAFF
              </NavLink>
            )}
            <button
              type="button"
              onClick={toggleMute}
              style={{
                border: "1px solid #6b4a1c",
                background: "#2a1406",
                color: "#ffb15c",
                fontFamily: "var(--font-display)",
                fontSize: 12,
                letterSpacing: "1px",
                padding: "10px 14px",
                whiteSpace: "nowrap",
                cursor: "pointer",
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.borderColor = "#ff8a1f";
                e.currentTarget.style.boxShadow = "0 0 14px rgba(255,138,31,.4)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = "#6b4a1c";
                e.currentTarget.style.boxShadow = "none";
              }}
            >
              {muted ? "SND OFF" : "SND ON"}
            </button>
          </div>
        </header>'''
assert old in s, "header anchor not found"
s = s.replace(old, new, 1)

# RadialMenu call: no satellites, hub = back when drilled
old = '''            <RadialMenu nodes={nodes} satellites={satellites} hub={hub} />'''
new = '''            <RadialMenu
              nodes={nodes}
              hub={hub}
              onHubActivate={activeGroup ? () => setDrilled(null) : undefined}
            />'''
assert old in s, "radial call anchor not found"
s = s.replace(old, new, 1)

# hub status line advertises the back affordance when drilled
old = '''            : activeGroup
              ? `\u25c6 ${activeGroup.label} \u25c6`
              : "\u25c6 PICK YOUR GAME \u25c6"}'''
new = '''            : activeGroup
              ? "\u25c0 CLICK HUB TO GO BACK"
              : "\u25c6 PICK YOUR GAME \u25c6"}'''
assert old in s, "hub status anchor not found"
s = s.replace(old, new, 1)

io.open(p, "w", encoding="utf-8", newline="").write(s)
print("patched part 2:", p)
