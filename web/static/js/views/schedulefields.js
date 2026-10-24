// Reset schedule inputs (timezone, time, weekday, day of month) shared by the key modal and the
// global dialog on the Quotas page (ADR 0010). The pure rules live in ../resetschedule.js.
import { h, searchableSelect, timeZone } from '../ui.js';
import { WEEKDAYS, neededFields, validateSchedule, defaultSchedule } from '../resetschedule.js';

// Every IANA zone the browser knows, UTC first. Falls back to a short list on old engines.
export function timezoneOptions(extra = []) {
  let zones = [];
  try {
    zones = Intl.supportedValuesOf('timeZone');
  } catch {
    zones = ['Asia/Jakarta', 'Asia/Singapore', 'Asia/Tokyo', 'Europe/London', 'Europe/Berlin', 'America/New_York', 'America/Los_Angeles', 'Australia/Sydney'];
  }
  const list = [...new Set(['UTC', ...extra.filter(Boolean), ...zones])];
  return list.map((z) => ({ value: z, label: z }));
}

// A group of schedule inputs. `onChange` fires on any edit. Which of weekday/day show follows
// setNeeded({time, weekday, day}). value() reads the inputs; set(s) fills them.
export function scheduleFields({ initial, onChange = () => {} }) {
  const start = initial || defaultSchedule(null, timeZone());
  const tz = searchableSelect({
    placeholder: 'Timezone', searchPlaceholder: 'Search timezones...', ariaLabel: 'Timezone', value: start.timezone,
    options: timezoneOptions([start.timezone, timeZone()]), clearable: false, compact: false, wide: true,
    onChange: () => onChange(),
  });
  const time = h('input', { class: 'input', type: 'time', step: '60', value: start.time, 'aria-label': 'Reset time (24h)', style: { width: '130px' } });
  const weekday = h('select', { class: 'input', 'aria-label': 'Reset weekday', style: { width: '170px' } },
    WEEKDAYS.map(([n, label]) => h('option', { value: String(n), selected: n === Number(start.weekday) }, label)));
  const day = h('input', { class: 'input', type: 'number', min: '1', max: '31', step: '1', value: String(start.day), 'aria-label': 'Reset day of month', style: { width: '90px' } });
  [time, weekday, day].forEach((el) => {
    el.addEventListener('input', () => onChange());
    el.addEventListener('change', () => onChange());
  });

  const field = (label, node, hint) => h('div', { class: 'field', style: { marginBottom: 0 } },
    h('span', { style: { fontSize: '11.5px', fontWeight: '600' } }, label), node,
    hint ? h('small', { class: 'muted', style: { fontSize: '11px' } }, hint) : null);
  const timeBox = field('Time (24h)', time);
  const weekdayBox = field('Weekday', weekday, 'Weekly quotas reset on this day.');
  const dayBox = field('Day of month', day, 'Monthly quotas. Short months reset on their last day (Feb 28/29, Apr 30...).');
  const node = h('div', { style: { display: 'flex', flexDirection: 'column', gap: '10px' } },
    field('Timezone', tz, 'Daylight saving is followed automatically.'),
    h('div', { style: { display: 'flex', gap: '14px', flexWrap: 'wrap', alignItems: 'flex-start' } }, timeBox, weekdayBox, dayBox));

  return {
    node,
    value: () => ({ timezone: tz.value, time: time.value, weekday: Number(weekday.value), day: Number(day.value) }),
    set(s) {
      tz.setValue(s.timezone, false);
      time.value = s.time;
      weekday.value = String(s.weekday);
      day.value = String(s.day);
    },
    problem() { return validateSchedule(this.value()); },
    setNeeded(need) {
      timeBox.style.display = need.time === false ? 'none' : '';
      weekdayBox.style.display = need.weekday ? '' : 'none';
      dayBox.style.display = need.day ? '' : 'none';
    },
    setDisabled(disabled) {
      tz.disabled = disabled;
      [time, weekday, day].forEach((el) => { el.disabled = disabled; });
    },
  };
}

export { neededFields };
