// Run: node --test web/jstest/
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  KEY_LIST_DEFAULTS, stateFromParams, paramsFromState, apiQuery, nextSort,
  withFilter, totalPages, rangeLabel, pageItems, pageAfterReload,
} from '../static/js/keylist.js';

test('stateFromParams defaults and validation', () => {
  assert.deepEqual(stateFromParams({}), { ...KEY_LIST_DEFAULTS });
  assert.deepEqual(
    stateFromParams({ page: '3', limit: '50', sort: 'tokens', order: 'asc', q: 'pi', status: 'active', mode: 'group' }),
    { page: 3, limit: 50, sort: 'tokens', order: 'asc', q: 'pi', status: 'active', mode: 'group' },
  );
  assert.deepEqual(
    stateFromParams({ page: '0', limit: '7', sort: 'bogus', order: 'up', status: 'x', mode: 'y' }),
    { ...KEY_LIST_DEFAULTS },
  );
});

test('paramsFromState omits defaults and round-trips', () => {
  assert.deepEqual(paramsFromState(KEY_LIST_DEFAULTS), {});
  const s = { ...KEY_LIST_DEFAULTS, page: 2, q: 'pi', status: 'active' };
  assert.deepEqual(paramsFromState(s), { page: '2', q: 'pi', status: 'active' });
  assert.deepEqual(stateFromParams(paramsFromState(s)), s);
});

test('apiQuery always pages and omits empty filters', () => {
  assert.deepEqual(apiQuery(KEY_LIST_DEFAULTS), { page: 1, limit: 25, sort: 'last_active', order: 'desc' });
  assert.deepEqual(
    apiQuery({ ...KEY_LIST_DEFAULTS, q: 'pi', status: 'disabled', mode: 'custom' }),
    { page: 1, limit: 25, sort: 'last_active', order: 'desc', q: 'pi', status: 'disabled', mode: 'custom' },
  );
});

test('nextSort toggles and resets page', () => {
  const s = { ...KEY_LIST_DEFAULTS, page: 4 };
  assert.deepEqual(nextSort(s, 'name'), { ...s, sort: 'name', order: 'desc', page: 1 });
  assert.equal(nextSort(s, 'last_active').order, 'asc');
  assert.equal(nextSort({ ...s, order: 'asc' }, 'last_active').order, 'desc');
});

test('withFilter resets page', () => {
  assert.deepEqual(withFilter({ ...KEY_LIST_DEFAULTS, page: 5 }, { q: 'x' }).page, 1);
});

test('totalPages and rangeLabel', () => {
  assert.equal(totalPages(0, 25), 1);
  assert.equal(totalPages(112, 25), 5);
  assert.equal(rangeLabel(2, 25, 112, 25), 'Showing 26\u201350 of 112');
  assert.equal(rangeLabel(5, 25, 112, 12), 'Showing 101\u2013112 of 112');
  assert.equal(rangeLabel(1, 25, 0, 0), 'No keys');
});

test('pageItems with ellipses', () => {
  assert.deepEqual(pageItems(1, 5), [1, 2, 3, 4, 5]);
  assert.deepEqual(pageItems(1, 10), [1, 2, '\u2026', 10]);
  assert.deepEqual(pageItems(5, 10), [1, '\u2026', 4, 5, 6, '\u2026', 10]);
  assert.deepEqual(pageItems(10, 10), [1, '\u2026', 9, 10]);
  assert.deepEqual(pageItems(3, 10), [1, 2, 3, 4, '\u2026', 10]);
});

test('pageAfterReload steps back from an emptied page', () => {
  assert.equal(pageAfterReload(3, 0), 2);
  assert.equal(pageAfterReload(1, 0), 1);
  assert.equal(pageAfterReload(3, 4), 3);
});
