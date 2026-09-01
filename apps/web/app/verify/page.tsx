import type { Metadata } from "next";
import Link from "next/link";
import VerifyClient from "@/components/VerifyClient";

export const metadata: Metadata = {
  title: "Verify — Retro Casino",
  description:
    "Recompute a slot outcome in your browser from the revealed server seed, client seed, and nonce. No server involved.",
};

export default function VerifyPage() {
  return (
    <div className="mx-auto max-w-[1128px] px-4 pt-6 pb-12">
      <header className="mb-6 flex items-center justify-between gap-4">
        <h1 className="font-display text-2xl text-magenta">VERIFY A SPIN</h1>
        <Link href="/" className="font-display text-base text-cyan">
          ◂ BACK TO THE FLOOR
        </Link>
      </header>

      <div className="border-4 border-stone bg-shadow p-4 shadow-hard">
        <p className="m-0 mb-4 text-base text-haze">
          This page recomputes the outcome entirely in your browser with
          WebCrypto. It never calls the API — that independence is what makes
          the check meaningful. Enter the revealed server seed (shown when you
          rotate your seed pair), the client seed, and the nonce from your bet
          history.
        </p>
        <VerifyClient />
      </div>
    </div>
  );
}
