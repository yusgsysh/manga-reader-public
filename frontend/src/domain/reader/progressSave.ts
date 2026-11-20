const pendingSaves = new Set<Promise<unknown>>();

const MAX_SETTLE_WAIT_MS = 5_000;

export function trackProgressSave(save: Promise<unknown>): void {
  pendingSaves.add(save);
  const settle = () => {
    pendingSaves.delete(save);
  };
  save.then(settle, settle);
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

export function whenProgressSavesSettled(): Promise<void> {
  if (pendingSaves.size === 0) return Promise.resolve();

  const waitForPending = async (): Promise<void> => {
    while (pendingSaves.size > 0) {
      await Promise.allSettled([...pendingSaves]);
    }
  };

  return Promise.race([waitForPending(), delay(MAX_SETTLE_WAIT_MS)]);
}
