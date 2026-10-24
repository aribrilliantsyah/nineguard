// Global Reset Schedule dialog on the Quotas page (ADR 0010): one schedule for every API key
// that has no schedule of its own. Saving keeps usage; each key's current window ends at its
// next reset under the new schedule.
import { api } from '../api.js';
import { h, formDialog, confirmDialog, toast, timeZone } from '../ui.js';
import { defaultSchedule, nextResetText, validateSchedule } from '../resetschedule.js';
import { scheduleFields } from './schedulefields.js';

// Opens the dialog. `onChanged` runs after anything was saved so the page can reload.
export async function openGlobalScheduleDialog({ onChanged = () => {} } = {}) {
  let state;
  try {
    state = await api.get('/settings/quota-reset-schedule');
  } catch (e) {
    toast(`Could not load the reset schedule: ${e.message}`, 'err');
    return;
  }

  let changed = false;
  const current = state.quota_reset_schedule || null;
  let enabled = Boolean(current);
  const fields = scheduleFields({ initial: current || defaultSchedule(null, timeZone()), onChange: () => scheduleEdited() });

  const useDefault = h('input', { type: 'radio', name: 'gs-mode' });
  const useCustom = h('input', { type: 'radio', name: 'gs-mode' });
  useDefault.checked = !enabled;
  useCustom.checked = enabled;
  useDefault.addEventListener('change', () => { enabled = false; render(); });
  useCustom.addEventListener('change', () => { enabled = true; render(); });
  const choice = (input, title, sub) => h('label', { style: { display: 'flex', gap: '8px', alignItems: 'flex-start', cursor: 'pointer' } },
    input, h('span', null, h('b', null, title), h('div', { class: 'muted', style: { fontSize: '11.5px' } }, sub)));

  const counts = h('div', { style: { fontSize: '12.5px' } });
  const clearRow = h('div', { style: { display: 'flex', alignItems: 'center', gap: '10px' } });
  const nextLine = h('div', { style: { fontSize: '12.5px' } });
  const problem = h('div', { class: 'form-error', style: { margin: 0 } });

  function drawCounts(s) {
    counts.replaceChildren(
      h('b', null, `Applies to ${s.following_keys} key${s.following_keys === 1 ? '' : 's'}. `),
      h('span', { class: 'muted' }, s.custom_keys === 0 ? 'No key has its own schedule.'
        : `${s.custom_keys} key${s.custom_keys === 1 ? ' has its' : 's have their'} own schedule and ${s.custom_keys === 1 ? 'is' : 'are'} not affected.`));
    // replaceChildren(null) would render the text "null", so only real nodes are passed.
    clearRow.replaceChildren(...(s.custom_keys > 0 ? [
      h('button', { class: 'btn btn-sm', type: 'button', onclick: () => clearOverrides(s.custom_keys) }, `Clear overrides (${s.custom_keys})`),
      h('span', { class: 'muted', style: { fontSize: '11.5px' } }, 'Makes every key follow this schedule. Usage is kept.'),
    ] : []));
  }

  async function clearOverrides(n) {
    const ok = await confirmDialog({
      title: 'Clear overrides?',
      body: `${n} key${n === 1 ? '' : 's'} will stop using ${n === 1 ? 'its' : 'their'} own schedule and follow the global one. Usage is kept; each key's current window ends at its next reset.`,
      confirmText: 'Clear overrides',
    });
    if (!ok) return;
    try {
      const s = await api.post('/settings/quota-reset-schedule/clear-overrides', {});
      state = s; changed = true;
      drawCounts(s);
      toast('Overrides cleared', 'ok');
    } catch (e) {
      toast(`Could not clear overrides: ${e.message}`, 'err');
    }
  }

  let seq = 0;
  let timer = null;
  function scheduleEdited() {
    problem.textContent = enabled ? validateSchedule(fields.value()) : '';
    clearTimeout(timer);
    timer = setTimeout(refreshNext, 250);
  }

  async function refreshNext() {
    const mine = ++seq;
    const sched = enabled ? fields.value() : null;
    if (enabled && validateSchedule(sched)) { nextLine.textContent = ''; return; }
    let p;
    try { p = await api.post('/quotas/reset-preview', { quota_reset_schedule: sched }); } catch { if (mine === seq) nextLine.textContent = ''; return; }
    if (mine !== seq) return;
    const tz = sched ? sched.timezone : 'UTC';
    nextLine.replaceChildren(
      h('div', { class: 'muted', style: { fontSize: '11.5px', marginBottom: '2px' } }, 'Next resets under this schedule'),
      h('div', null, 'Daily: ', h('b', null, nextResetText(p.daily, tz))),
      h('div', null, 'Weekly: ', h('b', null, nextResetText(p.weekly, tz))),
      h('div', null, 'Monthly: ', h('b', null, nextResetText(p.monthly, tz))),
      h('div', { class: 'muted', style: { fontSize: '11.5px', marginTop: '4px' } }, 'Usage is kept; each key\'s current window ends at its next reset under this schedule.'));
  }

  function render() {
    fields.node.style.display = enabled ? '' : 'none';
    scheduleEdited();
  }

  drawCounts(state);
  render();

  const body = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '12px' } },
    counts,
    choice(useDefault, 'No global schedule', 'Keys without their own schedule reset at 00:00 UTC (Monday, the 1st).'),
    choice(useCustom, 'Set a global schedule', 'One schedule for every key that has none of its own.'),
    fields.node, problem, nextLine, clearRow);

  await formDialog({
    title: 'Reset schedule',
    fields: [{ node: body }],
    submitText: 'Save',
    wide: true,
    onSubmit: async () => {
      if (enabled) {
        const bad = validateSchedule(fields.value());
        if (bad) throw new Error(bad);
      }
      const s = await api.post('/settings/quota-reset-schedule', { quota_reset_schedule: enabled ? fields.value() : null });
      state = s; changed = true;
      toast(enabled ? 'Global reset schedule saved' : 'Global reset schedule removed', 'ok');
      return true;
    },
  });
  clearTimeout(timer);
  if (changed) onChanged();
}
