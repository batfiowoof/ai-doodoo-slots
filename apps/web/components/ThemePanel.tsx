"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSetTheme, useTheme } from "@/lib/theme";

interface ThemeDTO {
  id: number;
  name: string;
  palette: string[];
  sprites: { name: string; rows: string[] }[];
  createdAt: string;
}

export default function ThemePanel() {
  const qc = useQueryClient();
  const setActive = useSetTheme();
  const [prompt, setPrompt] = useState("");
  const [error, setError] = useState<string | null>(null);

  const themes = useQuery({
    queryKey: ["themes"],
    queryFn: async (): Promise<ThemeDTO[]> => {
      const res = await fetch("/api/v1/themes");
      if (!res.ok) throw new Error(`themes failed: ${res.status}`);
      return res.json() as Promise<ThemeDTO[]>;
    },
  });

  const generate = useMutation({
    mutationFn: async (p: string): Promise<ThemeDTO> => {
      const res = await fetch("/api/v1/themes", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ prompt: p }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => null)) as { message?: string } | null;
        throw new Error(body?.message ?? `theme failed: ${res.status}`);
      }
      return res.json() as Promise<ThemeDTO>;
    },
  });

  const submit = () => {
    setError(null);
    if (prompt.trim().length === 0 || prompt.length > 200) {
      setError("prompt must be 1-200 characters");
      return;
    }
    generate.mutate(prompt.trim(), {
      onSuccess: (t) => {
        void qc.invalidateQueries({ queryKey: ["themes"] });
        // The server validated the theme before storing, so applying is
        // safe. A malformed generation is rejected server-side, never
        // reaches here, and the machine keeps its previous theme.
        setActive({ id: t.id, name: t.name, palette: t.palette, sprites: t.sprites });
      },
      onError: (err) => {
        setError(err instanceof Error ? err.message : "generation failed");
      },
    });
  };

  return (
    <section className="border-4 border-stone bg-shadow p-4 shadow-hard">
      <h2 className="m-0 mb-4 border-b-4 border-slate pb-2 font-display text-base text-cyan">
        THEME MACHINE
      </h2>

      <div className="flex gap-2">
        <input
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && submit()}
          maxLength={200}
          placeholder="sunken pirate arcade"
          spellCheck={false}
          className="min-w-0 flex-1 border-4 border-slate bg-ink p-2 font-body text-xl text-bone focus:border-cyan focus:outline-none"
        />
        <button
          type="button"
          onClick={submit}
          disabled={generate.isPending}
          className="border-4 border-plum bg-violet p-2 font-display text-base text-white disabled:cursor-not-allowed disabled:border-slate disabled:bg-stone disabled:text-haze"
        >
          {generate.isPending ? "…" : "GENERATE"}
        </button>
      </div>

      {error && (
        <p role="alert" className="m-0 mt-2 border-4 border-ember bg-ink p-2 text-ember">
          {error} · your machine keeps its current theme
        </p>
      )}

      {themes.isLoading && <p className="mt-4 text-haze">loading…</p>}
      {themes.isError && <p className="mt-4 text-ember">themes unavailable</p>}

      {themes.data && themes.data.length > 0 && (
        <ThemeList themes={themes.data} />
      )}
    </section>
  );
}

function ThemeList({ themes }: { themes: ThemeDTO[] }) {
  const active = useTheme();
  const setActive = useSetTheme();

  return (
    <div className="mt-4 flex flex-col gap-2">
      {themes.map((t) => (
        <div
          key={t.id}
          className={`flex items-center justify-between gap-4 border-4 p-2 ${
            active?.id === t.id ? "border-mint bg-ink" : "border-slate bg-ink"
          }`}
        >
          <span className="min-w-0 truncate text-xl">{t.name}</span>
          <button
            type="button"
            onClick={() =>
              setActive({ id: t.id, name: t.name, palette: t.palette, sprites: t.sprites })
            }
            disabled={active?.id === t.id}
            className="shrink-0 border-4 border-slate bg-stone px-2 py-1 font-display text-base text-bone disabled:text-haze"
          >
            {active?.id === t.id ? "ACTIVE" : "APPLY"}
          </button>
        </div>
      ))}
      {active && (
        <button
          type="button"
          onClick={() => setActive(null)}
          className="self-start border-0 bg-transparent p-0 font-display text-base text-haze"
        >
          ◂ back to factory glyphs
        </button>
      )}
    </div>
  );
}
