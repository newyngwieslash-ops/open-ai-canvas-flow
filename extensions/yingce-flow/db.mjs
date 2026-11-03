let database;
function open() {
  if (!database) database = new Promise((resolve, reject) => {
    const request = indexedDB.open('yingce-flow-v1', 1);
    request.onupgradeneeded = () => {
      for (const name of ['transfers', 'chunks', 'files', 'bundles', 'runs']) request.result.createObjectStore(name);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => { database = undefined; reject(request.error); };
  });
  return database;
}

export async function read(store, key) {
  const db = await open();
  return new Promise((resolve, reject) => {
    const request = db.transaction(store).objectStore(store).get(key);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

export async function all(store) {
  const db = await open();
  return new Promise((resolve, reject) => {
    const request = db.transaction(store).objectStore(store).getAll();
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

export async function write(store, key, value) {
  return transact([{ store, key, value }]);
}

export async function transact(changes) {
  const db = await open();
  return new Promise((resolve, reject) => {
    const transaction = db.transaction([...new Set(changes.map(c => c.store))], 'readwrite');
    for (const change of changes) {
      const store = transaction.objectStore(change.store);
      if (change.remove) store.delete(change.key); else store.put(change.value, change.key);
    }
    transaction.oncomplete = () => resolve();
    transaction.onerror = transaction.onabort = () => reject(transaction.error || new Error('本地存储事务失败'));
  });
}

export async function removeTransfer(transfer) {
  const changes = [{ store: 'transfers', key: transfer.id, remove: true }];
  for (const media of transfer.bundle.media) for (let i = 0; i < Math.ceil(media.byteLength / 262144); i++) {
    changes.push({ store: 'chunks', key: [transfer.id, media.id, i], remove: true });
  }
  await transact(changes);
}
