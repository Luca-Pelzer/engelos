<script lang="ts">
  import { Handle, Position, type NodeProps } from '@xyflow/svelte';

  let { data, selected }: NodeProps = $props();

  const kind = $derived((data.kind as string) ?? 'action');
  const title = $derived((data.title as string) ?? '');
  const subtitle = $derived((data.subtitle as string) ?? '');
  const disabled = $derived((data.disabled as boolean) ?? false);
</script>

<div class="canvas-node {kind}" class:disabled class:selected>
  {#if kind !== 'trigger'}<Handle type="target" position={Position.Left} />{/if}
  <span class="node-kind">{kind === 'trigger' ? 'WHEN' : kind === 'condition' ? 'IF' : 'DO'}</span>
  <div class="node-body">
    <div class="node-title">{title}</div>
    {#if subtitle}<div class="node-sub">{subtitle}</div>{/if}
  </div>
  {#if kind !== 'action-last'}<Handle type="source" position={Position.Right} />{/if}
</div>

<style>
  .canvas-node {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 190px;
    max-width: 260px;
    padding: 10px 14px;
    border-radius: 12px;
    background: var(--panel-bg);
    border: 1px solid var(--panel-border);
    box-shadow: 0 4px 18px rgb(0 0 0 / 0.25);
    font-family: inherit;
    cursor: pointer;
    transition: border-color 0.12s ease, box-shadow 0.12s ease;
  }
  .canvas-node.selected {
    border-color: var(--brand) !important;
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--brand) 45%, transparent), 0 4px 18px rgb(0 0 0 / 0.3);
  }
  .canvas-node.trigger { border-color: var(--brand); }
  .canvas-node.condition { border-color: var(--brand-2); }
  .canvas-node.disabled { opacity: 0.5; }
  .node-kind {
    flex: 0 0 auto;
    font-size: 0.6rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    padding: 3px 7px;
    border-radius: 6px;
    color: #fff;
    background: var(--brand);
  }
  .canvas-node.condition .node-kind { background: var(--brand-2); }
  .canvas-node.trigger .node-kind { background: linear-gradient(135deg, var(--brand), var(--brand-2)); }
  .node-body { min-width: 0; }
  .node-title { font-size: 0.82rem; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .node-sub { font-size: 0.7rem; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
