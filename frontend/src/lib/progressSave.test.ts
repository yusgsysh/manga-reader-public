import { describe, expect, it } from "vitest";
import { trackProgressSave, whenProgressSavesSettled } from "./progressSave";

function macrotask(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0);
  });
}

describe("whenProgressSavesSettled", () => {
  it("resolves immediately when no save is pending", async () => {
    await expect(whenProgressSavesSettled()).resolves.toBeUndefined();
  });

  it("waits until the pending save settles", async () => {
    let finish!: () => void;
    trackProgressSave(
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
    );

    let settled = false;
    const waiting = whenProgressSavesSettled().then(() => {
      settled = true;
    });

    await macrotask();
    expect(settled).toBe(false);

    finish();
    await waiting;
    expect(settled).toBe(true);
  });

  it("keeps waiting when a new save starts while waiting", async () => {
    let finishFirst!: () => void;
    trackProgressSave(
      new Promise<void>((resolve) => {
        finishFirst = resolve;
      }),
    );

    let settled = false;
    const waiting = whenProgressSavesSettled().then(() => {
      settled = true;
    });

    let finishSecond!: () => void;
    trackProgressSave(
      new Promise<void>((resolve) => {
        finishSecond = resolve;
      }),
    );

    finishFirst();
    await macrotask();
    expect(settled).toBe(false);

    finishSecond();
    await waiting;
    expect(settled).toBe(true);
  });

  it("settles even when a save rejects", async () => {
    let fail!: (error: Error) => void;
    trackProgressSave(
      new Promise<never>((_resolve, reject) => {
        fail = reject;
      }),
    );

    const waiting = whenProgressSavesSettled();
    fail(new Error("network down"));
    await expect(waiting).resolves.toBeUndefined();
  });
});
