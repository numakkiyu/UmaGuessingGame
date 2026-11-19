<script setup lang="ts">
import { computed, ref } from 'vue';
import { useData, withBase } from 'vitepress';
import games from './games.json';
const props = defineProps<{ compact?: boolean }>();
const { lang } = useData();
const language = computed(() => lang.value === 'ja' ? 'ja' : lang.value === 'zh-Hant' ? 'zh-Hant' : 'zh-Hans');
const index = computed(() => ['zh-Hans','zh-Hant','ja'].indexOf(language.value));
const prefix = computed(() => language.value === 'zh-Hans' ? '/' : `/${language.value}/`);
const selection = ref(-1);
const categories = [['全部玩法','角色知识','观察与记忆','历史与关联','编队与策略','聚会与合作'],['所有玩法','角色知識','觀察與記憶','歷史與關聯','編隊與策略','聚會與合作'],['すべて','ウマ娘の知識','観察と記憶','レースとつながり','編成と作戦','パーティーと協力']];
const visible = computed(() => props.compact ? games.filter(g => ['classic','memory','audio','clue-puzzle','strategy-race','creative'].includes(g.id)) : games.filter(g => selection.value < 0 || g.category === selection.value));
</script>
<template>
  <div class="mode-gallery">
    <div v-if="!compact" class="gallery-filters" role="group" :aria-label="categories[index][0]">
      <button v-for="(category,i) in categories[index]" :key="category" type="button" :aria-pressed="selection === i-1" @click="selection=i-1">{{ category }}</button>
    </div>
    <div class="gallery-grid">
      <a v-for="game in visible" :key="game.id" class="gallery-game" :href="withBase(`${prefix}play/${game.id}`)">
        <div class="gallery-frame"><img :src="withBase(`/media/play/${game.id}-${language}-thumb.webp`)" :alt="game.titles[index]" width="720" height="450" loading="lazy" /></div>
        <div class="gallery-copy"><span class="gallery-number" aria-hidden="true">{{ String(games.indexOf(game)+1).padStart(2,'0') }}</span><h3>{{ game.titles[index] }}</h3><span class="gallery-arrow" aria-hidden="true">↗</span><p>{{ game.summaries[index] }}</p><span v-if="game.id === 'bluff'" class="gallery-status">{{ ['题库核对中','題庫核對中','問題を確認中'][index] }}</span></div>
      </a>
    </div>
  </div>
</template>
