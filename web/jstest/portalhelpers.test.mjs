import test from 'node:test';
import assert from 'node:assert/strict';

import {
  isValidKeyFormat,
  maskKey,
  buildHeaders,
  getStoredPortalKey,
  setStoredPortalKey,
  clearStoredPortalKey
} from '../static/js/portalhelpers.js';

test('isValidKeyFormat validates key structure', () => {
  assert.equal(isValidKeyFormat(''), false);
  assert.equal(isValidKeyFormat('   '), false);
  assert.equal(isValidKeyFormat(null), false);
  assert.equal(isValidKeyFormat(undefined), false);
  assert.equal(isValidKeyFormat('invalid-key'), false);
  assert.equal(isValidKeyFormat('sk-ng-abc1234567890'), true);
  assert.equal(isValidKeyFormat('sk-proj-xyz123'), true);
  assert.equal(isValidKeyFormat('Bearer sk-ng-abc1234'), true);
});

test('maskKey masks token safely', () => {
  assert.equal(maskKey(''), '-');
  assert.equal(maskKey(null), '-');
  assert.equal(maskKey('sk-1234'), 'sk-...34');
  assert.equal(maskKey('sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
  assert.equal(maskKey('Bearer sk-ng-1234567890abcdef'), 'sk-ng-...cdef');
});

test('buildHeaders constructs authorization header', () => {
  assert.deepEqual(buildHeaders('sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });

  // Strips leading 'Bearer ' if user input already includes it
  assert.deepEqual(buildHeaders('Bearer sk-ng-secret123'), {
    'Authorization': 'Bearer sk-ng-secret123'
  });
});

test('localStorage key management', () => {
  const mockStorage = new Map();
  const fakeLocalStorage = {
    getItem: (k) => mockStorage.get(k) || null,
    setItem: (k, v) => mockStorage.set(k, String(v)),
    removeItem: (k) => mockStorage.delete(k)
  };

  assert.equal(getStoredPortalKey(fakeLocalStorage), '');

  setStoredPortalKey('  sk-ng-test123  ', fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), 'sk-ng-test123');

  clearStoredPortalKey(fakeLocalStorage);
  assert.equal(getStoredPortalKey(fakeLocalStorage), '');
});
