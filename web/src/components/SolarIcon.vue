<template>
  <!-- Bootstrap Icons has no solar-panel glyph, so the nav uses this inline SVG.
       Inherits currentColor and the surrounding font size. -->
  <svg
    class="solar-panel-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <rect x="3.2" y="4.2" width="17.6" height="11" rx="1.4" />
    <path d="M9.1 4.2v11M14.9 4.2v11M3.2 9.7h17.6" />
    <path d="M12 15.2v3.6M8.4 18.8h7.2" />
  </svg>
</template>

<style scoped>
.solar-panel-icon {
  display: block;
  width: 1em;
  height: 1em;
}
</style>
