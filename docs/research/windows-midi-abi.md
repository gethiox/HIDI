# Windows MIDI Services WinRT ABI facts for a pure-Go virtual device

Researched 2026-11-16. Authoritative source: `github.com/microsoft/MIDI`, `main` at commit `9e043b1ac13a9b4e0f320866310e7f49fe2403d0` (2026-11-13), obtained with a plain `git clone` (works through the proxy).

URL shorthand used below:
- `GH/<path>` = `https://github.com/microsoft/MIDI/blob/9e043b1ac13a9b4e0f320866310e7f49fe2403d0/<path>`
- `CORE/` = `GH/src/in-box/Client/WinRT/core/`
- `DEFS` = `GH/src/in-box/Inc/midi_sdk_idl_defs.h` (every IID below comes from this file unless noted)
- `SITE/<page>` = `https://microsoft.github.io/MIDI/<page>` (live site reachable by `curl`; pages checked match the repo `docs/` tree)

Access notes: `learn.microsoft.com` is BLOCKED by the session proxy (CONNECT 403) so nothing here comes from it. The GitHub REST API (`gh api`) is not enabled for `microsoft/MIDI` in this session; not needed since git clone works.

Confidence legend:
- VERIFIED-IDL: read directly from the IDL / header in the repo.
- CONFIRMED-BY-SAMPLE: an official sample hand-declares the same ABI.
- DERIVED: vtable slot numbers and ABI C signatures derived from the IDL using the standard MIDL 3.0 / WinRT ABI rules (see section 2.0). Not read from a generated header or .winmd, because the repo checks in neither. Verify against the .winmd before trusting slot numbers for anything not CONFIRMED-BY-SAMPLE.
- UNVERIFIED: could not be confirmed.

---

## 1. Namespace, packaging and activation

### 1.1 What is current

- Current and only supported API: WinRT namespace `Windows.Devices.Midi2`, implemented by one DLL, `Windows.Devices.Midi2.dll`. "Use `Windows.Devices.Midi2`. It is part of Windows, in box in Windows 11 25H2 and later starting in 2027, and it is the only supported Windows MIDI Services API target if you intend to ship." Source: `GH/docs/kb/porting-midi-libraries.md` (section "Target the in-box API"), live at `SITE/kb/porting-midi-libraries/`.
- The old namespace `Microsoft.Windows.Devices.Midi2` "was the out-of-band preview package. It was never supported for redistribution, only for testing against the preview, and it is being dropped entirely in 2027." Type names are "largely identical"; never mix. Same source.
- Virtual device types moved: old `Microsoft.Windows.Devices.Midi2.Endpoints.Virtual` -> new `Windows.Devices.Midi2.Transports.Virtual`. In the old SDK `MidiDeclaredEndpointInfo` was a `struct`; in the new API it is a `runtimeclass` (so there are NO fixed-layout structs for endpoint info / device identity / function block any more, see 2.9). Sources: tag `rc-4` `src/app-sdk/winrt/MidiVirtualDevice.idl`, `MidiDeclaredEndpointInfo.idl` vs `CORE/MidiVirtualDevice.idl`, `CORE/MidiDeclaredEndpointInfo.idl`.

### 1.2 Windows builds

Source for all rows: `GH/docs/kb/minimum-requirements.md`, `GH/docs/kb/porting-midi-libraries.md`, `GH/samples/cpp-winrt/detect-midi-services/main_detect_midi_services.cpp`.

| Windows | Build | Status |
| --- | --- | --- |
| < Windows 11 24H2 | < 26100 | No Windows MIDI Services. Use WinMM / WinRT MIDI 1.0. |
| Windows 11 24H2 | 26100 | Has an older MIDI service (WinMM goes through it) but "never gets the API". Docs say: do not use the API there "even if a copy of it would load". |
| Windows 11 25H2+ | >= 26200 | API supported. As of this research the in-box copy of `Windows.Devices.Midi2.dll` ships to consumers "starting in 2027" via Windows update; until then the only copy is the one from the `Windows.Devices.Midi2` NuGet package (current: `0.99.97/98-preview.14`), deployed next to the app's .exe. |

24H2 and 25H2 "run the same code, so nothing else tells them apart": check the build number with `RtlGetVersion` (not `GetVersionEx`). The service-side virtual-device ("APP") transport is listed as shipped in box (`GH/docs/overview/index.md`).

Which exact Windows update/KB carries the in-box DLL: UNVERIFIED (docs only say "in 2027" / "comes in a Windows update").

### 1.3 Activation for an unpackaged desktop app (new API)

No bootstrapper, no initializer COM object, no "SDK runtime" installer. It is plain WinRT activation of classes in `Windows.Devices.Midi2.dll`. Two cases (source: comments in `GH/samples/cpp-winrt/detect-midi-services/main_detect_midi_services.cpp` and `GH/samples/app-manifest/app.manifest`):

1. In-box copy present (class registered by Windows): `RoGetActivationFactory(HSTRING className, REFIID iid, void** factory)` just works. "When Windows has its own copy, that's the one your app gets, even if the app ships a copy too."
2. Only the app-local NuGet copy is present (not registered): `RoGetActivationFactory` returns `REGDB_E_CLASSNOTREG` (0x80040154). Two documented fixes:
   - (a) Call the DLL export directly. The official sample does exactly this as the fallback:
     ```cpp
     auto const module = ::LoadLibraryExW(L"Windows.Devices.Midi2.dll", nullptr,
         LOAD_LIBRARY_SEARCH_SYSTEM32 | LOAD_LIBRARY_SEARCH_APPLICATION_DIR);
     using DllGetActivationFactoryProc = HRESULT(__stdcall*)(HSTRING, ::IActivationFactory**);
     auto const getActivationFactory = reinterpret_cast<DllGetActivationFactoryProc>(
         ::GetProcAddress(module, "DllGetActivationFactory"));
     getActivationFactory(name, factory.put());            // returns IActivationFactory*
     factory->QueryInterface(__uuidof(abi::IMidiApiStatics), statics.put_void());
     ```
     Export confirmed in `CORE/midi-app-winrt-sdk.def` (`DllGetActivationFactory`, `DllCanUnloadNow`). This path needs no manifest and is pure-Go friendly (`LoadLibraryEx` + `GetProcAddress` + `SyscallN`).
   - (b) Registration-free WinRT manifest: embed `<file name="Windows.Devices.Midi2.dll"><activatableClass name="..." threadingModel="both" xmlns="urn:schemas-microsoft-com:winrt.v1"/>...</file>` in the exe manifest; then `RoGetActivationFactory`/`RoActivateInstance` work "from any language". Complete class list in `GH/samples/app-manifest/app.manifest` (made from package 0.99.91-preview.11). It includes all classes this scenario activates by name: `Windows.Devices.Midi2.MidiApi`, `.MidiClock`, `.MidiSession`, `.MidiEndpointConnection`, `.MidiGroup`, `Windows.Devices.Midi2.Enumeration.MidiDeclaredEndpointInfo`, `.MidiFunctionBlock`, `.MidiDeclaredDeviceIdentity`, `.MidiEndpointUserSuppliedInfo`, `Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDeviceCreationConfig`, `.MidiVirtualDeviceManager`. `MidiVirtualDevice` is not in the list (it only comes back from `CreateVirtualDevice`).
   Recommended order for Go: try `RoGetActivationFactory`; on failure fall back to (a).

Startup sequence documented (`GH/docs/kb/moving-from-winmm-to-wms.md`, `GH/samples/cpp-winrt/simple-app-to-app-midi/main_simple_app_to_app.cpp`): init apartment (MTA recommended) -> `MidiApi.EnsureServiceAvailable()` must return true (it demand-starts `midisrv`; returns false in Legacy API mode or if the service is disabled) -> `MidiSession.Create(...)`.

`MidiApi` statics (CONFIRMED-BY-SAMPLE, the sample declares these by hand with these IIDs and this order):

| Interface | IID | Slot | ABI |
| --- | --- | --- | --- |
| `Windows.Devices.Midi2.IMidiApiStatics` | `8087b303-0519-c0de-31d1-ee0010000000` | 6 | `HRESULT EnsureServiceAvailable(boolean* result)` |
| | | 7 | `HRESULT GetCurrentlySelectedApiMode(INT32* result)` (0 = FullWindowsMidiServicesMode, 1 = LegacyMode, 2 = HybridLegacyMode; `CORE/MidiApiModeEnum.idl`, `CORE/midi_api_mode_defs.h`) |
| `Windows.Devices.Midi2.IMidiApiStatics2` | `8087b303-0519-c0de-31d1-ee0010000002` | 6 | `HRESULT IsSystemProvided(boolean* result)` (true = in-box copy; QI may fail on very old app-local copies) |

`MidiApi` is a static class: get its activation factory and QI; `RoActivateInstance` does not work on it.

### 1.4 Legacy activation (old `Microsoft.Windows.Devices.Midi2` SDK, for reference only; being dropped in 2027)

Source: tag `rc-4` (2026-04-12) `src/app-sdk/client-initialization-redist/Microsoft.Windows.Devices.Midi2.Initialization.hpp` and `docs/kb/how-to-bootstrap-windows-midi-services.md`.

- Requires the centrally installed "Windows MIDI Services App SDK runtime" (`https://aka.ms/MidiServicesLatestSdkRuntimeInstaller`). A COM bootstrapper hooks (detours) WinRT activation so no manifest is needed.
- `CoCreateInstance(CLSID {c3263827-c3b0-bdbd-2500-ce63a3f3f2c3}, NULL, CLSCTX_INPROC_SERVER | CLSCTX_FROM_DEFAULT_CONTEXT, IID {8087b303-d551-bce2-1ead-a2500d50c580}, &p)`; creating it initializes the runtime. `IMidiClientInitializer : IUnknown` vtable: slot 3 `GetInstalledWindowsMidiServicesSdkVersion(MidiAppSDKPlatform*, USHORT*, USHORT*, USHORT*, LPWSTR*, LPWSTR*, LPWSTR*)`, slot 4 `EnsureServiceAvailable()`. Keep the pointer alive for the life of MIDI use; release it last.
- Service-installed probe: `CoCreateInstance(CLSID {2BA15E4E-5417-4A66-85B8-2B2260EFBC84} (MidiSrvTransport), CLSCTX_INPROC_SERVER, IUnknown)`.
- The legacy interface IIDs are NOT listed here; they are in that tag's `src/app-sdk/...` defs header and differ from the table below in namespace (and possibly value). Do not assume they match. UNVERIFIED for equality.

---

## 2. Classes, interfaces, IIDs, vtable order

### 2.0 Rules used for the DERIVED slot numbers and signatures

- All these are WinRT interfaces deriving from `IInspectable`: slots 0-2 `QueryInterface/AddRef/Release`, 3-5 `GetIids/GetRuntimeClassName/GetTrustLevel`, first own method at slot 6.
- MIDL 3.0 synthesizes, per runtimeclass, a default instance interface (`[interface_name]`), a statics interface (`[static_name]`) and a factory interface (`[constructor_name]`), each containing that kind of member in IDL declaration order. A read/write property becomes `get_X` then `put_X` (adjacent); an event becomes `add_X` then `remove_X`; overloads stay in declaration order. A parameterless constructor is NOT on the factory interface: it is `IActivationFactory::ActivateInstance` (IID `00000035-0000-0000-C000-000000000046`, slot 6, `HRESULT ActivateInstance(IInspectable** instance)`), then QI for the default interface. The repo header says the same: "the class has only a default constructor, which activates through IActivationFactory, so midlrt emits no factory interface" (`DEFS`).
- The official sample states the principle and shows it holds for `IMidiApiStatics`: "Method order is the vtable order and must match the metadata exactly."
- `[noexcept]` only affects projections; at the ABI every method still returns `HRESULT` and the logical return value is a trailing out pointer (CONFIRMED-BY-SAMPLE for `MidiApi`).
- ABI types: `String` = `HSTRING` (in: by value; out: `HSTRING*`, caller frees with `WindowsDeleteString`). `Boolean` = 1-byte `boolean`. `UInt8` = `BYTE`. `Guid` = 16-byte `GUID` BY VALUE as in-param (x64: pointer to a caller copy in the arg slot; ARM64: passed in two general registers), `GUID*` as out. Runtimeclass params = pointer to that class's DEFAULT interface. Interface params = pointer to exactly that interface (QI first). Enums = `INT32`, `[flags]` enums = `UINT32`. `T[]` in-param = `(UINT32 length, T* data)`.
- IID pattern (`DEFS`): `8087b303-0519-c0de-31d1-` + `cc..` standalone interface, `dd..` default interface, `ee..` statics, `ff..` factory.
- The IDLs for every file listed in this section are byte-identical between tags `inbox-preview-11` (2026-10-11), `-12`, `-13` and `main` (checked with `git diff`). API contracts are all version 1 for Core/Enumeration/Virtual (`CORE/MidiApiContracts.idl`). It is still a preview: re-verify on the shipping .winmd.

How to verify at run time / build time without guessing: read `Windows.Devices.Midi2.winmd` (NuGet `ref\native\`, later the Windows SDK) with any ECMA-335 reader (`System.Reflection.Metadata`, ILSpy, `winmdidl`, or the repo's own `GH/src/in-box/Client/WinRT/build-tools/BuildClassMetadataForManifestAndTests.ps1`): each interface TypeDef carries a `GuidAttribute` and its MethodDefs are in vtable order. At run time, `IInspectable::GetIids` on any returned object lists the IIDs it implements, and `GetRuntimeClassName` confirms the class.

### 2.1 `Windows.Devices.Midi2.MidiSession`  (`CORE/MidiSession.idl`, VERIFIED-IDL; slots DERIVED)

Implements: `IMidiSession` (default), `Windows.Foundation.IClosable` (`30D5A829-7FA4-4026-83BB-D75BAE4EA99E`, slot 6 `HRESULT Close()`), `Windows.Foundation.IStringable` (`96369F54-8EB6-48F0-ABCE-C1B211E627C3`). Activatable (statics only).

`IMidiSessionStatics` `8087b303-0519-c0de-31d1-ee0010009000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT Create(HSTRING sessionName, IMidiSession** result)` (null result = failure) |

`IMidiSession` `8087b303-0519-c0de-31d1-dd0010009000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT get_SessionId(GUID* value)` |
| 7 | `HRESULT get_Name(HSTRING* value)` |
| 8 | `HRESULT get_IsOpen(boolean* value)` |
| 9 | `HRESULT get_Connections(IMapView<Guid, MidiEndpointConnection>** value)` |
| 10 | `HRESULT CreateEndpointConnection(HSTRING endpointDeviceId, IMidiEndpointConnection** result)` |
| 11 | `HRESULT CreateEndpointConnection(HSTRING endpointDeviceId, IMidiEndpointConnectionSettings* settings, IMidiEndpointConnection** result)` (ABI name of the 2nd overload unknown/irrelevant for vtable calls) |
| 12 | `HRESULT DisconnectEndpointConnection(GUID endpointConnectionId)` |
| 13 | `HRESULT UpdateName(HSTRING newName, boolean* result)` |

Docs (`GH/docs/sdk-reference/MidiSession.md`): `CreateEndpointConnection` returns a connection that "isn't open yet: attach your event handlers, and then call `Open()`". Close a connection with `session.DisconnectEndpointConnection(connectionId)` (not the connection's own IClosable), then `IClosable::Close` on the session.

### 2.2 `Windows.Devices.Midi2.MidiEndpointConnection`  (`CORE/MidiEndpointConnection.idl`)

Implements: `IMidiEndpointConnection` (default), `IMidiMessageReceivedEventSource`, `IMidiEndpointConnectionSource`, `IStringable`; also classic-COM `IMidiEndpointConnectionRaw` (2.11).

`IMidiEndpointConnectionStatics` `8087b303-0519-c0de-31d1-ee0010006000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT GetDeviceSelector(HSTRING* result)` |
| 7 | `HRESULT SendMessageSucceeded(UINT32 sendResult, boolean* result)` |
| 8 | `HRESULT SendMessageFailed(UINT32 sendResult, boolean* result)` |

`IMidiEndpointConnection` `8087b303-0519-c0de-31d1-dd0010006000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT get_LogMessageDataValidationErrorDetails(boolean* value)` |
| 7 | `HRESULT put_LogMessageDataValidationErrorDetails(boolean value)` |
| 8 | `HRESULT Open(boolean* result)` |
| 9 | `HRESULT get_MessageProcessingPlugins(IVectorView<IMidiEndpointMessageProcessingPlugin>** value)` |
| 10 | `HRESULT AddMessageProcessingPlugin(IMidiEndpointMessageProcessingPlugin* plugin, INT32* result)` (result enum 2.8) |
| 11 | `HRESULT RemoveMessageProcessingPlugin(GUID id)` |
| 12 | `HRESULT SendSingleMessagePacket(IMidiUniversalPacket* message, UINT32* result)` |
| 13 | `HRESULT SendSingleMessageStruct(UINT64 timestamp, UINT8 wordCount, const MidiMessageStruct* message, UINT32* result)` (`ref const` struct = pointer) |
| 14 | `HRESULT SendSingleMessageWordArray(UINT64 timestamp, UINT32 startIndex, UINT8 wordCount, UINT32 wordsLength, UINT32* words, UINT32* result)` |
| 15 | `HRESULT SendSingleMessageWords(UINT64 timestamp, UINT32 word0, UINT32* result)`  <- the one for a 32-bit MT 0x2 UMP |
| 16 | `HRESULT SendSingleMessageWords(UINT64 timestamp, UINT32 word0, UINT32 word1, UINT32* result)` |
| 17 | `HRESULT SendSingleMessageWords(UINT64 timestamp, UINT32 word0, UINT32 word1, UINT32 word2, UINT32* result)` |
| 18 | `HRESULT SendSingleMessageWords(UINT64 timestamp, UINT32 word0, UINT32 word1, UINT32 word2, UINT32 word3, UINT32* result)` |
| 19 | `HRESULT SendSingleMessageBuffer(UINT64 timestamp, UINT32 byteOffset, UINT8 byteCount, IMemoryBuffer* buffer, UINT32* result)` |
| 20 | `HRESULT SendMultipleMessagesWordList(UINT64 timestamp, IIterable<UInt32>* words, UINT32* result)` |
| 21 | `HRESULT SendMultipleMessagesWordArray(UINT64 timestamp, UINT32 startIndex, UINT32 wordCount, UINT32 wordsLength, UINT32* words, UINT32* result)` |
| 22 | `HRESULT SendMultipleMessagesPacketList(IIterable<IMidiUniversalPacket>* messages, UINT32* result)` |
| 23 | `HRESULT SendMultipleMessagesStructList(UINT64 timestamp, IIterable<MidiMessageStruct>* messages, UINT32* result)` |
| 24 | `HRESULT SendMultipleMessagesStructArray(UINT64 timestamp, UINT32 startIndex, UINT32 messageCount, UINT32 messagesLength, MidiMessageStruct* messages, UINT32* result)` |
| 25 | `HRESULT SendMultipleMessagesBuffer(UINT64 timestamp, UINT32 byteOffset, UINT32 byteCount, IMemoryBuffer* buffer, UINT32* result)` |
| 26 | `HRESULT GetSupportedMaxMidiWordsPerTransmission(UINT32* result)` |

Implementation note (`CORE/MidiEndpointConnection_SendSingleMessage.cpp`): the one-word overload rejects a word whose message type is not a 32-bit UMP with `Failed | InvalidMessageTypeForWordCount`; MT 0x2 is 32-bit so it passes. Slots 15-18 are the highest-risk DERIVED slots (four overloads plus array params before them): confirm on the .winmd, or avoid the question entirely with slot 12/13/14 or the COM extension in 2.11.

`MidiMessageStruct` (`CORE/MidiMessageStruct.idl`, VERIFIED-IDL): `{ UINT32 Word0; UINT32 Word1; UINT32 Word2; UINT32 Word3; }`, 16 bytes, align 4.

`IMidiEndpointConnectionSource` `8087b303-0519-c0de-31d1-cc001000F030` (`CORE/IMidiEndpointConnectionSource.idl`)
| Slot | ABI |
| --- | --- |
| 6 / 7 | `add_EndpointDeviceDisconnected(TypedEventHandler<IMidiEndpointConnectionSource, IInspectable>* h, EventRegistrationToken* token)` / `remove_...(EventRegistrationToken token)` (token = INT64) |
| 8 / 9 | `add_/remove_EndpointDeviceReconnected` (same shapes) |
| 10 / 11 | `get_Tag(IInspectable**)` / `put_Tag(IInspectable*)` |
| 12 | `HRESULT get_ConnectionId(GUID* value)`  <- needed for `DisconnectEndpointConnection` |
| 13 | `HRESULT get_ConnectedEndpointDeviceId(HSTRING* value)` |
| 14 | `HRESULT get_Settings(IMidiEndpointConnectionSettings** value)` |
| 15 | `HRESULT get_IsOpen(boolean* value)` |

`IMidiMessageReceivedEventSource` `8087b303-0519-c0de-31d1-cc001000F050`: 6 `add_MessageReceived(TypedEventHandler<IMidiMessageReceivedEventSource, MidiMessageReceivedEventArgs>*, EventRegistrationToken*)`, 7 `remove_MessageReceived(EventRegistrationToken)`, 8 `GetEndpointConnectionSource(IMidiEndpointConnectionSource**)`. Not needed for send-only. If you ever implement the handler delegate in Go, its parameterized IID computed with the standard WinRT pinterface rule (UUIDv5/SHA-1, namespace `11f47ad5-7b73-42c0-abae-878b1e16adee`, signature `pinterface({9de1c534-6ae1-11e0-84e1-18a905bcc53f};{8087b303-0519-c0de-31d1-cc001000f050};rc(Windows.Devices.Midi2.MidiMessageReceivedEventArgs;{8087b303-0519-c0de-31d1-dd0010008000}))`) is `3c0d41fa-1439-538f-b525-c8767aae16a6` (COMPUTED by me; my implementation reproduces the known `IVector<String>` and `IIterable<String>` IIDs; still verify at run time).

`MidiEndpointConnectionSettings` (`CORE/MidiEndpointConnectionSettings.idl`): default `IMidiEndpointConnectionSettings` `8087b303-0519-c0de-31d1-dd0010007000` (6 `get_WaitForEndpointReceiptOnSend(boolean*)`, 7 `get_AutoReconnect(boolean*)`); factory `...-ff0010007000` (6 `CreateInstance(boolean, IMidiEndpointConnectionSettings**)`, 7 `CreateInstance(boolean, boolean, ...**)`); default ctor via `ActivateInstance`. Optional: use the one-arg `CreateEndpointConnection`.

### 2.3 `Windows.Devices.Midi2.IMidiEndpointMessageProcessingPlugin`  `8087b303-0519-c0de-31d1-cc001000F040`  (`CORE/IMidiEndpointMessageProcessingPlugin.idl`)

You do not call these; you only need the IID to QI the `MidiVirtualDevice` before passing it to `AddMessageProcessingPlugin` (the parameter is this interface, not the class's default interface).

Slots: 6 `get_PluginId(GUID*)`, 7/8 `get_/put_PluginName(HSTRING)`, 9/10 `get_/put_PluginTag(IInspectable)`, 11/12 `get_/put_IsEnabled(boolean)`, 13 `Initialize(IMidiEndpointConnectionSource*)`, 14 `OnEndpointConnectionOpened()`, 15 `ProcessIncomingMessage(MidiMessageReceivedEventArgs*, boolean* skipFurtherListeners, boolean* skipMainMessageReceivedEvent)`, 16 `Cleanup()`.

### 2.4 `Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDeviceManager` (static)  (`CORE/MidiVirtualDeviceManager.idl`)

`IMidiVirtualDeviceManagerStatics` `8087b303-0519-c0de-31d1-ee00F0404000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT get_IsTransportAvailable(boolean* value)` |
| 7 | `HRESULT get_TransportId(GUID* value)` (`{8FEAAD91-70E1-4A19-997A-377720A719C1}`, `GH/docs/kb/virtual-device-app.md`) |
| 8 | `HRESULT CreateVirtualDevice(IMidiVirtualDeviceCreationConfig* creationConfig, IMidiVirtualDevice** result)` (null result = failure; no error detail is returned, see `CORE/MidiVirtualDeviceManager.cpp`) |
| 9 | `HRESULT GetAssociatedClientEndpointDeviceId(GUID associationId, HSTRING* result)` (empty string = not found; only meaningful after the device connection is opened, since the client endpoint is created then) |

There is no RemoveVirtualDevice (commented out in the IDL): the device disappears when the owning connection/session closes.

### 2.5 `Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDeviceCreationConfig`  (`CORE/MidiVirtualDeviceCreationConfig.idl`)

Implements: `IMidiVirtualDeviceCreationConfig` (default), `Windows.Devices.Midi2.ServiceConfig.IMidiServiceTransportPluginConfig` (`8087b303-0519-c0de-31d1-cc0060006000`: 6 `get_TransportId(GUID*)`, 7 `get_ConfigJson(Windows.Data.Json.JsonObject**)`). No default constructor.

`IMidiVirtualDeviceCreationConfigFactory` `8087b303-0519-c0de-31d1-ff00F0403000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT CreateInstance(HSTRING name, HSTRING description, HSTRING manufacturer, IMidiDeclaredEndpointInfo* declaredEndpointInfo, IMidiVirtualDeviceCreationConfig** value)` |
| 7 | same + `IMidiDeclaredDeviceIdentity* declaredDeviceIdentity` before the out param |
| 8 | same + `IMidiDeclaredDeviceIdentity*`, `IMidiEndpointUserSuppliedInfo* userSuppliedInfo` before the out param |

`IMidiVirtualDeviceCreationConfig` `8087b303-0519-c0de-31d1-dd00F0403000`
| Slot | ABI |
| --- | --- |
| 6 / 7 | `get_Name(HSTRING*)` / `put_Name(HSTRING)` |
| 8 / 9 | `get_/put_Description` (HSTRING) |
| 10 / 11 | `get_/put_Manufacturer` (HSTRING) |
| 12 / 13 | `get_/put_CreateOnlyUmpEndpoints` (boolean; true = no WinMM / WinRT MIDI 1.0 ports are created, so leave false if other apps use the old APIs) |
| 14 | `HRESULT get_AssociationId(GUID* value)` (read-only, generated by the object) |
| 15 / 16 | `get_/put_DeclaredDeviceIdentity` (`IMidiDeclaredDeviceIdentity*`) |
| 17 / 18 | `get_/put_DeclaredEndpointInfo` (`IMidiDeclaredEndpointInfo*`) |
| 19 / 20 | `get_/put_UserSuppliedInfo` (`IMidiEndpointUserSuppliedInfo*`) |
| 21 | `HRESULT get_FunctionBlocks(IVector<MidiFunctionBlock>** value)` |

The product instance id is NOT a config parameter: it is `MidiDeclaredEndpointInfo.ProductInstanceId` and is what gets sent to the service as the endpoint's unique id (`CORE/MidiVirtualDeviceCreationConfig.cpp`). A servicing header named `Feature_Servicing_MIDI2VirtualDeviceRejectDuplicateUniqueId.h` exists in `src/in-box/Inc`, so use a unique product instance id per device (exact rejection behavior UNVERIFIED).

`IVector<MidiFunctionBlock>` (returned directly by slot 21, no QI needed): standard `IVector<T>` vtable: 6 `GetAt`, 7 `get_Size`, 8 `GetView`, 9 `IndexOf`, 10 `SetAt`, 11 `InsertAt`, 12 `RemoveAt`, 13 `Append(T value)`, 14 `RemoveAtEnd`, 15 `Clear`, 16 `GetMany`, 17 `ReplaceAll`. `Append` here is `HRESULT Append(IMidiFunctionBlock* value)`. Its parameterized IID (COMPUTED, same method as above; only needed if you QI): `4429fab8-13a5-5cb8-be75-292e782fd381`.

### 2.6 `Windows.Devices.Midi2.Transports.Virtual.MidiVirtualDevice`  (`CORE/MidiVirtualDevice.idl`)

Implements: `IMidiVirtualDevice` (default), `Windows.Devices.Midi2.IMidiEndpointMessageProcessingPlugin` (2.3). Not activatable; only returned by `CreateVirtualDevice`.

`IMidiVirtualDevice` `8087b303-0519-c0de-31d1-dd00F0402000`
| Slot | ABI |
| --- | --- |
| 6 | `HRESULT get_DeviceEndpointDeviceId(HSTRING* value)`  <- pass to `CreateEndpointConnection` |
| 7 | `HRESULT get_AssociationId(GUID* value)` |
| 8 | `HRESULT get_FunctionBlocks(IMapView<UInt8, MidiFunctionBlock>** value)` |
| 9 | `HRESULT UpdateFunctionBlock(IMidiFunctionBlock* block, boolean* result)` |
| 10 | `HRESULT UpdateEndpointName(HSTRING name, boolean* result)` |
| 11 | `HRESULT get_IsClientEndpointInUse(boolean* value)` |
| 12 | `HRESULT get_CapabilityInquiry(IMidiCapabilityInquiryDeviceResponder** value)` |
| 13 / 14 | `get_SuppressHandledMessages(boolean*)` / `put_SuppressHandledMessages(boolean)` (default true: discovery / stream-config messages the device answers are hidden from your `MessageReceived`) |
| 15 / 16 | `add_/remove_StreamConfigRequestReceived` (`TypedEventHandler<MidiVirtualDevice, MidiStreamConfigRequestReceivedEventArgs>`) |
| 17 / 18 | `add_/remove_ClientEndpointInUseChanged` (`TypedEventHandler<MidiVirtualDevice, MidiVirtualDeviceClientEndpointInUseChangedEventArgs>`) |

Caution: `IsClientEndpointInUse` and `CapabilityInquiry` sit in the middle of this interface. They are present in all four refs I compared (inbox-preview-11..13, main), but an older app-local DLL could lack them, which would shift slots >= 11. Slot 6 (the only one the minimal scenario needs) is first and therefore safe. Event-args interfaces: `IMidiStreamConfigRequestReceivedEventArgs` `...-dd00F0401000` (6 `get_Timestamp(UINT64*)`, 7 `get_PreferredMidiProtocol(INT32*)`, 8, 9 two `boolean*` getters), `IMidiVirtualDeviceClientEndpointInUseChangedEventArgs` `...-dd00F0405000` (6 `get_IsClientEndpointInUse(boolean*)`). Events are optional; you can ignore them for send-only.

### 2.7 Declared endpoint info, device identity, function block, group (all runtimeclasses, NOT structs)

There is no struct layout or size to match: these are heap COM objects accessed through getters/setters. The only by-value structs in the scenario are `MidiMessageStruct` (16 bytes) and `MidiSystemTimerSettings` (3 x UINT64 = 24 bytes).

`Windows.Devices.Midi2.Enumeration.MidiDeclaredEndpointInfo` (`CORE/MidiDeclaredEndpointInfo.idl`). Default ctor: `ActivateInstance`.

Factory `IMidiDeclaredEndpointInfoFactory` `8087b303-0519-c0de-31d1-ff0040012000`: slot 6 `HRESULT CreateInstance(HSTRING name, HSTRING productInstanceId, boolean supportsMidi10Protocol, boolean supportsMidi20Protocol, boolean supportsReceivingJitterReductionTimestamps, boolean supportsSendingJitterReductionTimestamps, boolean hasStaticFunctionBlocks, UINT8 declaredFunctionBlockCount, UINT8 specificationVersionMajor, UINT8 specificationVersionMinor, IMidiDeclaredEndpointInfo** value)`.

`IMidiDeclaredEndpointInfo` `8087b303-0519-c0de-31d1-dd0040012000`: 6 `get_IsReadOnly(boolean*)`; then get/put pairs: 7/8 `Name` (HSTRING), 9/10 `ProductInstanceId` (HSTRING), 11/12 `SupportsMidi10Protocol`, 13/14 `SupportsMidi20Protocol`, 15/16 `SupportsReceivingJitterReductionTimestamps`, 17/18 `SupportsSendingJitterReductionTimestamps`, 19/20 `HasStaticFunctionBlocks` (all boolean), 21/22 `DeclaredFunctionBlockCount`, 23/24 `SpecificationVersionMajor`, 25/26 `SpecificationVersionMinor` (all UINT8). The virtual device overwrites `DeclaredFunctionBlockCount` with the real number of function blocks when answering discovery (`CORE/MidiVirtualDevice.cpp`).

`Windows.Devices.Midi2.Enumeration.MidiDeclaredDeviceIdentity` (`CORE/MidiDeclaredDeviceIdentity.idl`, optional). Default `...-dd0040011000`, factory `...-ff0040011000` (slot 6 `CreateInstance(UINT8 x 11: sysexIdByte1..3, deviceFamilyLsb, deviceFamilyMsb, deviceFamilyModelNumberLsb, ...Msb, softwareRevisionLevelByte1..4, IMidiDeclaredDeviceIdentity** value)`). Instance: 6 `get_IsReadOnly`, 7 `get_SystemExclusiveId(UINT32* len, UINT8** data)` (receive-array, CoTaskMem), 8 `SetSystemExclusiveId(UINT8,UINT8,UINT8)`, 9 `get_DeviceFamilyLsb`, 10 `get_DeviceFamilyMsb`, 11 `SetDeviceFamily(UINT8,UINT8)`, 12 `get_DeviceFamilyModelNumberLsb`, 13 `...Msb`, 14 `SetDeviceFamilyModelNumber(UINT8,UINT8)`, 15 `get_SoftwareRevisionLevel(UINT32*, UINT8**)`, 16 `SetSoftwareRevisionLevel(UINT8 x4)`.

`Windows.Devices.Midi2.Enumeration.MidiEndpointUserSuppliedInfo` (optional): default `...-dd0040015000`, factory `...-ff0040015000`. See `CORE/MidiEndpointUserSuppliedInfo.idl`.

`Windows.Devices.Midi2.Enumeration.MidiFunctionBlock` (`CORE/MidiFunctionBlock.idl`). Only a default ctor: `ActivateInstance` then QI. Statics `...-ee004000f000` (four label getters, not needed).

`IMidiFunctionBlock` `8087b303-0519-c0de-31d1-dd004000f000`: 6 `get_IsReadOnly(boolean*)`; get/put pairs: 7/8 `Number` (UINT8), 9/10 `Name` (HSTRING), 11/12 `IsActive` (boolean), 13/14 `Direction` (INT32), 15/16 `UIHint` (INT32), 17/18 `RepresentsMidi10Connection` (INT32), 19/20 `FirstGroup` (`IMidiGroup*`), 21/22 `GroupCount` (UINT8); 23 `IncludesGroup(IMidiGroup* group, boolean* result)`; 24/25 `MidiCIMessageVersionFormat` (UINT8), 26/27 `MaxSystemExclusive8Streams` (UINT8).

Enums (VERIFIED-IDL): `MidiFunctionBlockDirection` Undefined 0, BlockInput 1 (a message destination), BlockOutput 2 (a message source), Bidirectional 3. `MidiFunctionBlockUIHint` Unknown 0, Receiver 1, Sender 2, Bidirectional 3. `MidiFunctionBlockRepresentsMidi10Connection` Not10 0, YesBandwidthUnrestricted 1, YesBandwidthRestricted 2, Reserved 3. `MidiProtocol` Default 0, Midi1 1, Midi2 2.

`Windows.Devices.Midi2.MidiGroup` (`CORE/MidiGroup.idl`): factory `IMidiGroupFactory` `8087b303-0519-c0de-31d1-ff0010002000` slot 6 `HRESULT CreateInstance(UINT8 index, IMidiGroup** value)` (index 0-15); default ctor via `ActivateInstance`. `IMidiGroup` `8087b303-0519-c0de-31d1-dd0010002000`: 6 `get_Index(UINT8*)`, 7 `put_Index(UINT8)`, 8 `get_DisplayValue(UINT8*)`. Statics `...-ee0010002000` not needed.

### 2.8 Result enums (VERIFIED-IDL, also `GH/docs/sdk-reference/MidiSendMessageResultsEnum.md`)

`MidiSendMessageResults` ([flags], UINT32): Succeeded `0x80000000`, Failed `0x10000000`, BufferFull `0x00010000`, EndpointConnectionClosedOrInvalid `0x00040000`, InvalidMessageTypeForWordCount `0x00100000`, InvalidMessageOther `0x00200000`, DataIndexOutOfRange `0x00400000`, TimestampOutOfRange `0x00800000`, TransmissionWordCountExceeded `0x01000000`.

Success test (exact implementation, `CORE/MidiEndpointConnection.h`): `(r & 0x80000000) == 0x80000000`; failure: `(r & 0x10000000) == 0x10000000`. Do this locally in Go; no need to call the statics. Remember the HRESULT is S_OK either way, the enum carries the outcome.

`MidiMessageProcessingPluginAddResult` (INT32): Succeeded 0, FailedPluginIsNull 1, FailedRawCallbackRegistered 2, FailedPluginAlreadyAdded 3, FailedPluginInitializationError 4.

### 2.9 `Windows.Devices.Midi2.MidiClock` (static)  (`CORE/MidiClock.idl`)

`IMidiClockStatics` `8087b303-0519-c0de-31d1-ee0010004000`: 6 `get_Now(UINT64*)`, 7 `get_TimestampConstantSendImmediately(UINT64*)`, 8 `get_TimestampConstantMessageQueueMaximumFutureTicks(UINT64*)`, 9 `get_TimestampFrequency(UINT64*)`, 10-13 `ConvertTimestampTicksTo{Nanoseconds,Microseconds,Milliseconds,Seconds}(UINT64, DOUBLE*)`, 14 `OffsetTimestampByTicks(UINT64, INT64, UINT64*)`, 15-17 `OffsetTimestampBy{Microseconds,Milliseconds,Seconds}(UINT64, INT64, UINT64*)`, 18 `GetCurrentSystemTimerInfo(MidiSystemTimerSettings*)`, 19 `BeginLowLatencySystemTimerPeriod(boolean*)`, 20 `EndLowLatencySystemTimerPeriod(boolean*)`.

Semantics (VERIFIED: `GH/docs/sdk-reference/MidiClock.md`, `GH/src/in-box/Inc/midi_timestamp.h`, `GH/src/in-box/Inc/MidiDefs.h`, `GH/docs/kb/moving-from-winmm-to-wms.md`):
- `Now` is literally `QueryPerformanceCounter`; `TimestampFrequency` is `QueryPerformanceFrequency` (currently 100 ns ticks). Absolute since boot, not relative.
- Timestamp `0` = send immediately: `#define MIDI_TIMESTAMP_SEND_IMMEDIATELY 0`; docs: "Its value is `0`, so you can also pass `0`", and it "bypasses an additional check in the service and so is ever so slightly more efficient" than passing `Now`.
- Max future scheduling: 5 minutes (`60*5 * frequency` ticks); beyond that the send fails with `TimestampOutOfRange`.
- So Go does not need to activate `MidiClock` at all for immediate sends: pass 0 (or call `QueryPerformanceCounter` directly, though docs recommend `MidiClock`).

### 2.10 `MidiMessage32` / `IMidiUniversalPacket` (only if you choose `SendSingleMessagePacket`; not recommended for Go)

`IMidiUniversalPacket` `8087b303-0519-c0de-31d1-cc001000F010`; `IMidiMessage32` `...-dd0010005032`, statics `...-ee0010005032`, factory `...-ff0010005032`. `MidiMessage32` is an `unsealed` runtimeclass, so its factory methods are very likely composable-style (extra outer/inner `IInspectable` params): factory ABI UNVERIFIED. Use `SendSingleMessageWords` instead.

### 2.11 COM extension (classic COM, IUnknown-based; simplest and least ambiguous send path)

Source: `GH/src/in-box/Client/WinRT/com-extensions-idl/WindowsMidiServicesAppSdkComExtensions.idl`, `SITE/sdk-reference/MidiEndpointConnection_COM-Extensions/`. `[object, local]`, declared order is the vtable order (plain MIDL), VERIFIED-IDL.

`IMidiEndpointConnectionRaw` IID `8087b303-0519-31d1-31d1-000000000020` (note: different 3rd/4th groups from the WinRT IIDs). QI it from the `MidiEndpointConnection` object.
| Slot | ABI |
| --- | --- |
| 3 | `UINT32 GetSupportedMaxMidiWordsPerTransmission()` (returns the value, not an HRESULT) |
| 4 | `BOOL ValidateBufferHasOnlyCompleteUmps(UINT32 wordCount, const UINT32* messages)` |
| 5 | `HRESULT SendMidiMessagesRaw(UINT64 timestamp, UINT32 wordCount, const UINT32* completeMessages)` |
| 6 | `HRESULT SetMessagesReceivedCallback(IMidiEndpointConnectionMessagesReceivedCallback* cb)` |
| 7 | `HRESULT RemoveMessagesReceivedCallback()` |

Callback interface IID `8087b303-0519-31d1-31d1-000000000010`. IMPORTANT for the virtual-device side: do NOT register the receive callback there. It is mutually exclusive with message processing plugins, and the virtual device is a plugin (`AddMessageProcessingPlugin` would return `FailedRawCallbackRegistered`). Docs: "A Virtual Device can still *send* through the COM Extensions." (`GH/docs/kb/virtual-device-app.md`). Release this pointer before closing the session / uninitializing the apartment.

---

## 3. Documented sequence (official C++/WinRT sample, trimmed)

Source: `GH/samples/cpp-winrt/simple-app-to-app-midi/main_simple_app_to_app.cpp` (C# equivalent: `GH/samples/csharp-net/virtual-device-app-winui/MainWindow.xaml.cs`, `Note.cs`).

```cpp
winrt::init_apartment();                                  // MTA by default
if (!MidiApi::EnsureServiceAvailable()) return 1;         // demand-starts midisrv
auto session = MidiSession::Create(L"Virtual Device C++ Session");

MidiDeclaredEndpointInfo endpointInfo;
endpointInfo.HasStaticFunctionBlocks(false);
endpointInfo.Name(L"MyApp");
endpointInfo.ProductInstanceId(L"3263827-8675309-5150");
endpointInfo.SupportsMidi10Protocol(true);
endpointInfo.SupportsMidi20Protocol(true);
endpointInfo.SupportsReceivingJitterReductionTimestamps(false);
endpointInfo.SupportsSendingJitterReductionTimestamps(false);
endpointInfo.SpecificationVersionMajor(1);
endpointInfo.SpecificationVersionMinor(1);

MidiVirtualDeviceCreationConfig creationConfig(
    endpointInfo.Name(), L"Virtual Device created from the C++ sample code", L"Samples R Us", endpointInfo);

// "If you want the function block names to also work as MIDI 1 port names then you need to keep the
//  names short ... In total, including null terminator, that needs to be 32 characters or fewer."
MidiFunctionBlock block0;
block0.Number(0);
block0.IsActive(true);
block0.Name(L"BiDi Function Block");
block0.FirstGroup(MidiGroup(static_cast<uint8_t>(0)));
block0.GroupCount(3);
block0.Direction(MidiFunctionBlockDirection::Bidirectional);
block0.RepresentsMidi10Connection(MidiFunctionBlockRepresentsMidi10Connection::Not10);
creationConfig.FunctionBlocks().Append(block0);

m_virtualDevice = MidiVirtualDeviceManager::CreateVirtualDevice(creationConfig);   // nullptr on failure

auto deviceEndpoint = session.CreateEndpointConnection(m_virtualDevice.DeviceEndpointDeviceId());
auto eventRevokeToken = deviceEndpoint.MessageReceived(MessageReceivedHandler);    // optional for send-only

// "this is what associates the virtual device client object with this connection. a virtual device is a
//  message processing plugin, so if this is not checked, a failure here gives you a device which sends
//  correctly and never receives anything."
if (deviceEndpoint.AddMessageProcessingPlugin(m_virtualDevice) != MidiMessageProcessingPluginAddResult::Succeeded) return 1;

// "Opening device endpoint connection. This will create the client-visible endpoint"
deviceEndpoint.Open();
...
session.DisconnectEndpointConnection(deviceEndpoint.ConnectionId());
session.Close();
m_virtualDevice = nullptr;   // release before exit, not during static destruction
```

Sending (from `GH/docs/sdk-reference/MidiSendMessageResultsEnum.md`):
```cpp
auto sendResult = myConnection.SendSingleMessageWords(MidiClock::TimestampConstantSendImmediately(), 0x28675309);
if (MidiEndpointConnection::SendMessageSucceeded(sendResult)) { ... }
```

UMP word for MIDI 1.0 channel voice (MT 0x2), per `GH/docs/kb/porting-midi-libraries.md` bit masks and `CORE/MidiMessageTypeEnum.idl` (`Midi1ChannelVoice32 = 0x2`): `word0 = 0x2<<28 | group<<24 | statusByte<<16 | data1<<8 | data2` (statusByte = opcode nibble | channel). The group must be one covered by a function block you declared as an output/bidirectional block for it to show up sensibly to other apps (and as a WinMM port).

Lifetime (`GH/docs/kb/virtual-device-app.md`): the device has two endpoints, the device-side one (only for the creating app) and the client-visible one. "Once the owning application disconnects from the endpoint, the Virtual UMP Device is torn down." The client-visible endpoint appears when the device-side connection is opened. Other apps using WinMM / WinRT MIDI 1.0 can see and use it (unless `CreateOnlyUmpEndpoints` is true); only the new API can host one.

Equivalent raw-ABI call order for Go:
1. `RoInitialize(RO_INIT_MULTITHREADED = 1)` on a locked OS thread.
2. Factory for `Windows.Devices.Midi2.MidiApi` -> QI `IMidiApiStatics` -> slot 6 `EnsureServiceAvailable`.
3. Factory `...Enumeration.MidiDeclaredEndpointInfo` -> either `IActivationFactory::ActivateInstance` + QI `IMidiDeclaredEndpointInfo` + setters, or QI `IMidiDeclaredEndpointInfoFactory` -> slot 6 with all values.
4. Factory `...Transports.Virtual.MidiVirtualDeviceCreationConfig` -> QI `IMidiVirtualDeviceCreationConfigFactory` -> slot 6.
5. For each function block: factory `...Enumeration.MidiFunctionBlock` -> `ActivateInstance` -> QI `IMidiFunctionBlock` -> setters; group from factory `Windows.Devices.Midi2.MidiGroup` -> QI `IMidiGroupFactory` -> slot 6. Config slot 21 `get_FunctionBlocks` -> `IVector::Append` (slot 13).
6. Factory `...Transports.Virtual.MidiVirtualDeviceManager` -> QI `IMidiVirtualDeviceManagerStatics` -> slot 8 `CreateVirtualDevice` -> `IMidiVirtualDevice*` (check non-null) -> slot 6 `get_DeviceEndpointDeviceId`.
7. Factory `Windows.Devices.Midi2.MidiSession` -> QI `IMidiSessionStatics` -> slot 6 `Create` -> slot 10 `CreateEndpointConnection(hstring)`.
8. QI the virtual device for `IMidiEndpointMessageProcessingPlugin` (`...cc001000F040`) -> connection slot 10 `AddMessageProcessingPlugin` (expect 0) -> slot 8 `Open` (expect true).
9. Send: connection slot 15 `SendSingleMessageWords(0, word0, &result)`, or QI `IMidiEndpointConnectionRaw` -> slot 5 `SendMidiMessagesRaw(0, 1, &word0)`.
10. Teardown: QI connection for `IMidiEndpointConnectionSource` -> slot 12 `get_ConnectionId` -> session slot 12 `DisconnectEndpointConnection(guid)` -> QI session `IClosable` -> slot 6 `Close`; release every pointer; then (optionally) `RoUninitialize`.

---

## 4. Things that can block or bite a pure-Go client

1. Availability (hard requirement): Windows 11 25H2 (build 26200)+, the MIDI service (`midisrv`) present and not disabled, and the PC not in Legacy API mode (`HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Drivers32\UseLegacyMidi` = 1). `EnsureServiceAvailable()` returning false covers the last two. Until the in-box DLL ships (2027), you must ship `Windows.Devices.Midi2.dll` from the NuGet package next to the exe, and the docs describe that package as a temporary preview ("Temporary package to use until the metadata is in an official Windows SDK", `GH/src/in-box/Client/WinRT/NuGet/Windows.Devices.Midi2.NuGet/nuget/Windows.Devices.Midi2.nuspec`). The package also ships `Windows.Devices.Midi2.pri` beside the DLL; the virtual device code loads a resource string for its plugin name (`ResourceGetHString(IDS_DISPLAY_VIRTUAL_DEVICE_NAME_PREFIX)` in `CORE/MidiVirtualDevice.cpp`), so deploy the .pri too. Whether a missing .pri breaks anything: UNVERIFIED.
2. Architectures: x64 and Arm64 only (Arm64X binary). No 32-bit (`GH/docs/kb/api-back-compat.md`, NuGet `.targets` platform check). A `GOARCH=386` build cannot use it.
3. Manifest: not required if you call `DllGetActivationFactory` directly (documented fallback) or once the in-box copy exists. Required only if you insist on `RoGetActivationFactory` with an app-local DLL; embedding a manifest in a Go exe needs a .syso resource.
4. .winmd: only needed by projection generators. Pure vtable calls need only the IIDs/slots above. No NuGet-only bootstrapper exists in the new API.
5. Apartment: per-thread. Docs recommend an owned thread initialized MTA; STA is allowed but calls are synchronous and service start "can take multiple seconds". No message pump is required in MTA. In Go: `runtime.LockOSThread()` and do `RoInitialize` plus all calls on that thread, or initialize MTA on every OS thread that touches these objects (all classes are registered `threadingModel="both"`). If `RoInitialize` returns `RPC_E_CHANGED_MODE` (thread already STA), do not treat as fatal.
6. Callbacks: receive events (`MessageReceived`, virtual device events) need Go-implemented COM delegate objects (`syscall.NewCallback`), invoked on service-owned threads not created by Go. For a send-only virtual device you can skip all of them; the `MidiVirtualDevice` plugin itself (implemented in the DLL) answers MIDI 2.0 endpoint discovery and function-block requests for you once added to the connection and opened.
7. GUID by value: `DisconnectEndpointConnection(GUID)`, `RemoveMessageProcessingPlugin(GUID)`, `GetAssociatedClientEndpointDeviceId(GUID)`. On amd64 pass a pointer to a GUID copy as the argument; on arm64 a 16-byte struct goes in two integer registers (pass the two 8-byte halves as two uintptr args). Getting this wrong corrupts the call. `boolean`/`UINT8` args occupy a full register slot; `UINT64` timestamp is one arg on both 64-bit targets.
8. HSTRING: create with `WindowsCreateString` (combase.dll) and free with `WindowsDeleteString`; a nil HSTRING is the empty string.
9. HRESULT is not the outcome: `[noexcept]` methods swallow errors and report through the return value (null object, false, enum). Check each out value: null `IMidiSession*`, null `IMidiVirtualDevice*`, `AddMessageProcessingPlugin != 0`, `Open == false`, send result without bit `0x80000000`.
10. Developer Mode: NOT required for client apps or virtual devices. It only relaxes the signing requirement for third-party service plugins (`GH/docs/overview/index.md`).
11. Packaged/UWP apps: marked "still investigating" (`GH/docs/kb/api-back-compat.md`); an unpackaged Win32 exe (what Go produces) is the supported case.
12. Shutdown ordering: release every COM reference before uninitializing the apartment or the process may crash at exit (`GH/docs/kb/porting-midi-libraries.md`, COM Extensions doc).
13. Preview churn: everything above is from a preview (`0.99.9x-preview.1x`). The IDLs were stable across the last four refs, and the repo forbids changing public API surface without sign-off (`GH/AGENTS.md`), but slot numbers should be re-verified against the .winmd that ships with the DLL you load. Cheap run-time guards: after each QI, call `GetRuntimeClassName` / `GetIids` and confirm the expected IID is present.

## 5. Explicit list of what is UNVERIFIED

- All vtable slot numbers except `IMidiApiStatics`/`IMidiApiStatics2` (sample-confirmed) and `IMidiEndpointConnectionRaw` (plain COM IDL): DERIVED from IDL order, not read from a generated header/.winmd.
- The two parameterized IIDs marked COMPUTED.
- `MidiMessage32` factory ABI (unsealed/composable).
- Which Windows update delivers the in-box DLL, and exact behavior on duplicate product instance ids or a missing .pri file.
- Anything on learn.microsoft.com (host blocked).
- Whether zero function blocks is accepted by the service (the samples always declare at least one; declare one).
