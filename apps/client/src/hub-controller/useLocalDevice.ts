import { t } from "../localization.ts";
import { useCallback, useEffect, useRef, useState } from "react";
import { deviceRequest } from "../localDevice";
import type { HubDiscovery, Request, Setup, Snapshot } from "./localProtocol";
import type { Connection } from "../platform";

const initialSetup = (): Setup => ({
  name: "",
  hub: true,
  executor: true,
  gateway: false,
  listen: "127.0.0.1:47830",
  hubUrl: "",
  invitation: "",
  useHubGateway: false,
  projects: [],
});

// Both first use and device settings share these actions. The native manager
// owns configuration and process rules; the view only sequences user actions.
export function useLocalDevice(
  open: boolean,
  onConnect: (connection: Connection) => Promise<void>,
  onClose: () => void,
  onExistingHub?: (url: string) => void,
  preferredHubURL?: string,
) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [setup, setSetup] = useState<Setup>(initialSetup);
  const [working, setWorking] = useState(false);
  const [reading, setReading] = useState(false);
  const [dataBusy, setDataBusy] = useState(false);
  const [error, setError] = useState("");
  const [phase, setPhase] = useState("");
  const [revision, setRevision] = useState(0);
  const [pairingMessage, setPairingMessage] = useState("");
  const [existingHub, setExistingHub] = useState<HubDiscovery | null>(null);
  const initialized = useRef(false);
  const operation = useRef(false);
  const paired = useRef<{ deviceId: string; connection: Connection } | null>(
    null,
  );

  const receive = useCallback(
    (value: Snapshot) => {
      setSnapshot(value);
      if (!initialized.current) {
        initialized.current = true;
        if (value.configured || value.setupPending)
          setSetup({
            name: value.name ?? t("我的电脑"),
            hub: value.hub,
            executor: value.executor,
            gateway: false,
            listen: value.listen ?? "127.0.0.1:47830",
            hubUrl: value.hubUrl ?? "",
            useHubGateway: false,
            projects: value.projects,
            invitation: "",
          });
        else
          setSetup({
            ...initialSetup(),
            hub: !preferredHubURL,
            hubUrl: preferredHubURL ?? "",
          });
      }
    },
    [preferredHubURL],
  );

  useEffect(() => {
    if (!open) return;
    let active = true;
    initialized.current = false;
    setReading(true);
    setError("");
    void deviceRequest({ operation: "status" })
      .then((result) => {
        if (active) receive(result.snapshot);
      })
      .catch((cause: Error) => {
        if (active) setError(cause.message);
      })
      .finally(() => {
        if (active) setReading(false);
      });
    return () => {
      active = false;
    };
  }, [open, receive]);

  useEffect(() => {
    let active = true;
    setExistingHub(null);
    if (
      open &&
      snapshot?.hub &&
      !snapshot.ready &&
      snapshot.operations?.includes("hub.discover")
    ) {
      void deviceRequest({ operation: "hub.discover" })
        .then((result) => {
          if (active) setExistingHub(result.existingHub ?? null);
        })
        .catch(() => {});
    }
    return () => {
      active = false;
    };
  }, [
    open,
    snapshot?.deviceId,
    snapshot?.baseUrl,
    snapshot?.hub,
    snapshot?.ready,
  ]);

  async function refresh() {
    const value = (await deviceRequest({ operation: "status" })).snapshot;
    receive(value);
    return value;
  }
  async function apply(request: Request) {
    if (["service.port", "model-routing.apply"].includes(request.operation))
      setError("");
    try {
      const value = await deviceRequest(request);
      receive(value.snapshot);
      if (request.operation === "service.port") {
        paired.current = null;
        setExistingHub(null);
      }
      if (
        [
          "setup",
          "repair.executor",
          "service.port",
          "model-routing.apply",
        ].includes(request.operation)
      )
        setRevision((value) => value + 1);
      return value;
    } catch (cause) {
      // Observe a possibly completed operation; never repeat the mutation here.
      await refresh().catch(() => {});
      throw cause;
    }
  }
  async function work(action: () => Promise<unknown>) {
    if (operation.current || dataBusy) return;
    operation.current = true;
    setWorking(true);
    setError("");
    try {
      await action();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      operation.current = false;
      setWorking(false);
      setPhase("");
    }
  }
  function updateSetup(value: Partial<Setup>) {
    setSetup((old) => ({ ...old, ...value }));
  }
  async function enter(current: Snapshot) {
    setPhase(t("正在连接工作台…"));
    let connection = paired.current?.connection;
    if (connection) {
      const now = await refresh();
      if (paired.current!.deviceId !== now.deviceId) {
        paired.current = null;
        throw new Error(t("本机设备已变化，请重新连接。"));
      }
      if (!now.ready || !now.baseUrl)
        throw new Error(t("本机网络尚未就绪，请启动服务后继续。"));
      connection = { ...connection, baseURL: now.baseUrl };
    } else {
      const result = await apply({
        operation: "connect",
        expectedDeviceId: current.deviceId,
      });
      if (!result.connection)
        throw new Error(t("本机服务没有返回输入端连接。"));
      connection = result.connection;
      paired.current = { deviceId: result.snapshot.deviceId!, connection };
    }
    await onConnect(connection);
    paired.current = null;
    onClose();
  }
  async function startAndEnter(current: Snapshot) {
    if (!current.ready && existingHub) {
      if (!onExistingHub)
        throw new Error(
          t("已发现 Hub，请连接 {0} 并登录。", existingHub.baseUrl),
        );
      onExistingHub(existingHub.baseUrl);
      return;
    }
    if (!current.ready) {
      setPhase(t("正在启动本机服务…"));
      const result = await apply({
        operation: "start",
        expectedDeviceId: current.deviceId,
      });
      if (result.existingHub) {
        setExistingHub(result.existingHub);
        if (!onExistingHub)
          throw new Error(
            t("已发现 Hub，请连接 {0} 并登录。", result.existingHub.baseUrl),
          );
        onExistingHub(result.existingHub.baseUrl);
        return;
      }
      current = result.snapshot;
    }
    if (current.hub) await enter(current);
  }
  async function saveSetup(launch: boolean) {
    await work(async () => {
      setPhase(t("正在保存本机配置…"));
      const value = await apply({
        operation: "setup",
        setup: {
          ...setup,
          gateway: false,
          hubUrl: setup.hub ? "" : setup.hubUrl,
          useHubGateway: false,
          projects: setup.executor ? setup.projects : [],
          account: setup.hub ? setup.account : undefined,
        },
      });
      updateSetup({ invitation: "", account: undefined });
      if (launch) await startAndEnter(value.snapshot);
    });
  }
  async function repair(invitation: string) {
    await work(async () => {
      setPairingMessage("");
      updateSetup({ invitation: "" });
      await apply({
        operation: "repair.executor",
        expectedDeviceId: snapshot?.deviceId,
        pairing: { invitation },
      });
      setPairingMessage(t("本机执行已连接到设备网络，可以启动服务。"));
    });
  }
  const restored = useCallback(() => {
    paired.current = null;
    initialized.current = false;
    setPairingMessage("");
    setSetup((value) => ({ ...value, invitation: "" }));
    setRevision((value) => value + 1);
  }, []);
  const upgraded = useCallback(() => setRevision((value) => value + 1), []);
  return {
    snapshot,
    setup,
    working,
    reading,
    dataBusy,
    busy: working || reading || dataBusy,
    error,
    phase,
    revision,
    pairingMessage,
    existingHub,
    receive,
    connectionPending: !!paired.current,
    setDataBusy,
    refresh,
    apply,
    work,
    updateSetup,
    saveSetup,
    repair,
    restored,
    upgraded,
    start: () =>
      work(async () => {
        setPhase(t("正在启动本机服务…"));
        await apply({
          operation: "start",
          expectedDeviceId: snapshot?.deviceId,
        });
      }),
    enter: () => snapshot && work(() => startAndEnter(snapshot)),
    stop: () =>
      work(async () => {
        setPhase(t("正在停止本机服务…"));
        await apply({
          operation: "stop",
          expectedDeviceId: snapshot?.deviceId,
          expectedInstanceId: snapshot?.instanceId,
        });
      }),
  };
}

export type LocalDeviceController = ReturnType<typeof useLocalDevice>;
