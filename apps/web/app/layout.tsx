import type { Metadata } from "next";
import { Silkscreen, VT323 } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";
import SocialDock from "@/components/SocialDock";

const silkscreen = Silkscreen({
  variable: "--font-silkscreen",
  weight: ["400", "700"],
  subsets: ["latin"],
});

const vt323 = VT323({
  variable: "--font-vt323",
  weight: "400",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Retro Casino — Play for Fun",
  description:
    "A play-money arcade casino. Credits have no cash value: no deposits, no cash-out, just pixels.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${silkscreen.variable} ${vt323.variable}`}>
      <body>
        <Providers>
          {children}
          {/* Global social chrome: chat, emotes, leaderboard, presence. */}
          <SocialDock />
        </Providers>
      </body>
    </html>
  );
}
