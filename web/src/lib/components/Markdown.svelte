<script lang="ts" module>
    import { marked } from 'marked';
    import DOMPurify from 'dompurify';

    // Open links in a new tab without leaking the opener.
    DOMPurify.addHook('afterSanitizeAttributes', (node) => {
        if (node.tagName === 'A') {
            node.setAttribute('target', '_blank');
            node.setAttribute('rel', 'noopener noreferrer');
        }
    });

    export function renderMarkdown(source: string): string {
        return DOMPurify.sanitize(marked.parse(source ?? '', { async: false, gfm: true, breaks: true }) as string);
    }
</script>

<script lang="ts">
    let { source }: { source: string } = $props();
    let html = $derived(renderMarkdown(source));
</script>

<div class="prose-md break-words">
    {@html html}
</div>
