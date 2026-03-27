import { GameHeader } from "@/components/game-header";
import { SiteFooter } from "@/components/site-footer";
import { getPublicConfig } from "@/config/public";
import { loadQuestionBankReady } from "@/lib/question-bank";

export default async function Home() {
  const questionBank = await loadQuestionBankReady();
  const config = getPublicConfig();

  return (
    <main className="relative mx-auto flex min-h-screen w-full max-w-[1320px] flex-col px-4 py-4 sm:px-6 lg:px-8">
      <div className="pointer-events-none absolute inset-x-6 top-0 -z-10 h-72 rounded-b-[56px] bg-[linear-gradient(180deg,rgba(151,216,28,0.16),rgba(237,247,255,0))]" />
      <div className="pointer-events-none absolute inset-x-10 top-6 -z-10 h-[300px] rounded-[40px] bg-[url('/assets/ui/backgrounds/bwiki-main-bg.png')] bg-cover bg-center opacity-[0.08] blur-[2px]" />
      <div className="pointer-events-none absolute left-0 right-0 top-20 -z-10 h-px bg-[linear-gradient(90deg,rgba(115,192,22,0),rgba(115,192,22,0.3),rgba(63,136,247,0.26),rgba(115,192,22,0))]" />

      <GameHeader
        questionBankSize={questionBank.length}
        maxGuesses={config.maxGuesses}
        turnstileEnabled={config.turnstileEnabled}
        turnstileSiteKey={config.turnstileSiteKey}
        featureFlags={config.featureFlags}
      />
      <SiteFooter className="mt-5" />
    </main>
  );
}
