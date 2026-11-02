import { useCallback, useEffect, useRef, useState } from "react";
import { deviceRequest } from "./localDevice";
import { commandID } from "@mewdex/sdk";
import type { JobInfo, JobRequest, Result, Snapshot } from "./localProtocol";

export const operationActive = (job: JobInfo | null | undefined) =>
  job?.state === "starting" || job?.state === "running";

// Jobs belong to the local installation, not the lifetime of a React view.
// Reads can recover a lost response. The creation request is sent exactly once.
export function useLocalOperation(onSnapshot: (value: Snapshot) => void) {
  const [job, setJob] = useState<JobInfo | null>(null);
  const [checking, setChecking] = useState(true);
  const [uncertain, setUncertain] = useState(false);
  const [error, setError] = useState("");
  const id = useRef<string | undefined>(undefined);
  const inFlight = useRef(false);
  const unconfirmed = useRef(false);
  const latest = useRef<JobInfo | null | undefined>(undefined);
  const mounted = useRef(true);

  const receive = useCallback(
    (result: Result) => {
      if (!mounted.current) return;
      const value = result.job ?? null;
      latest.current = value;
      id.current = value?.id;
      setJob(value);
      setUncertain(false);
      unconfirmed.current = false;
      onSnapshot(result.snapshot);
    },
    [onSnapshot],
  );

  const refresh = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      receive(
        await deviceRequest({
          operation: "job.status",
          job: id.current ? { id: id.current } : undefined,
        }),
      );
      if (mounted.current) setError("");
    } catch (cause) {
      if (mounted.current)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      inFlight.current = false;
      if (mounted.current) setChecking(false);
    }
  }, [receive]);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    const timer = setInterval(() => {
      // Read-only retries also cover a temporarily unavailable native helper.
      if (
        operationActive(latest.current) ||
        unconfirmed.current ||
        latest.current === undefined
      )
        void refresh();
    }, 1000);
    return () => {
      mounted.current = false;
      clearInterval(timer);
    };
  }, [refresh]);

  async function start(request: Omit<JobRequest, "id">) {
    if (
      inFlight.current ||
      checking ||
      uncertain ||
      operationActive(latest.current)
    )
      return false;
    const operationID = commandID().replaceAll("-", "");
    inFlight.current = true;
    id.current = operationID;
    unconfirmed.current = true;
    setUncertain(true);
    setError("");
    const jobRequest: JobRequest = {
      ...request,
      id: operationID,
      restore:
        request.operation === "restore.prepare"
          ? { ...request.restore, id: operationID }
          : request.restore,
      upgrade:
        request.operation === "upgrade.prepare"
          ? { id: operationID }
          : request.upgrade,
    };
    try {
      receive(await deviceRequest({ operation: "job.start", job: jobRequest }));
    } catch (cause) {
      // Retain the ID before observing; do not issue another job.start.
      if (mounted.current)
        setError(cause instanceof Error ? cause.message : String(cause));
      try {
        receive(
          await deviceRequest({
            operation: "job.status",
            job: { id: operationID },
          }),
        );
      } catch {
        /* The explicit refresh below can still recover the same operation. */
      }
    } finally {
      inFlight.current = false;
    }
    return true;
  }

  async function cancel() {
    if (inFlight.current || !id.current || !operationActive(latest.current))
      return;
    inFlight.current = true;
    try {
      receive(
        await deviceRequest({
          operation: "job.cancel",
          job: { id: id.current },
        }),
      );
      if (mounted.current) setError("");
    } catch (cause) {
      if (mounted.current)
        setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      inFlight.current = false;
    }
  }

  return {
    job,
    checking,
    uncertain,
    error,
    refresh,
    start,
    cancel,
    active: checking || uncertain || operationActive(job),
    reference: id.current,
  };
}
