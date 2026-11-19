import { useState, type CSSProperties } from "react";
import { BookOpen } from "lucide-react";
import { useTranslation } from "@/i18n";
import "@/i18n/stage";
import characters from "./stage-art.json";

export function CharacterStage() {
  const { t } = useTranslation("theme");
  const { t: stage } = useTranslation("stage");
  const [cast] = useState(() => {
    const choices = [...characters];
    for (let index = choices.length - 1; index > 0; index--) {
      const other = Math.floor(Math.random() * (index + 1));
      [choices[index], choices[other]] = [choices[other]!, choices[index]!];
    }
    return choices.slice(0, 3);
  });
  const [selected, setSelected] = useState(0);
  const active = cast[selected]!;
  return <div className="uma-character-stage">
    <div className="uma-character-orbit" aria-hidden="true" />
    <div className="uma-character-cast" aria-hidden="true">{cast.map((character, index) => <img key={character.id} src={`/assets/ui/characters/${character.key}.webp`} alt="" className={index === selected ? "is-front" : "is-back"} style={{ "--cast-slot": (index - selected + 3) % 3 } as CSSProperties} width={400} height={900} decoding="async" />)}</div>
    <figure className="uma-character-dialogue" key={active.key} aria-label={t("dialogue")}>
      <figcaption><span>{stage(`${active.key}.name`)}</span><a href={active.source} target="_blank" rel="noreferrer" aria-label={t("quoteSource")}><BookOpen aria-hidden="true" /></a></figcaption>
      <blockquote>{stage(`${active.key}.quote`)}</blockquote>
    </figure>
    <div className="uma-character-select">{cast.map((character, index) => <button type="button" key={character.id} aria-label={t("chooseCharacter", { name: stage(`${character.key}.name`) })} aria-pressed={index === selected} onClick={() => setSelected(index)}><img src={`/assets/ui/avatars/${character.id}.webp`} width={48} height={48} alt="" /></button>)}</div>
  </div>;
}
