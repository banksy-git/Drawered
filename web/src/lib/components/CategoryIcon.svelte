<script lang="ts">
    // Renders a Tabler Icons webfont glyph; unknown or missing names fall
    // back to a folder so the layout never collapses.
    let { icon, size = 16, class: cls = '' }: { icon: string | null | undefined; size?: number; class?: string } = $props();
</script>

<i
    class="ti ti-{icon || 'folder'} inline-block shrink-0 leading-none {icon ? '' : 'opacity-50'} {cls}"
    style="font-size: {size}px; width: {size}px"
    aria-hidden="true"
></i>
