; NomadWiFi installer (Inno Setup 6)
;
; Per-user by design: the app runs asInvoker, registers startup under HKCU, and
; keeps its state in the user profile. Installing under %LOCALAPPDATA% means no
; UAC prompt on install OR on update, which is what makes unattended updating
; possible at all -- a Program Files install would need elevation every time.
;
; Build:  iscc /DAppVersion=1.2.1 packaging\nomadwifi.iss
; The version is passed in by build.ps1 from the VERSION file.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

#define AppName        "NomadWiFi"
#define AppPublisher   "mory.dev"
#define AppURL         "https://nomadwifi.mory.dev"
#define AppExeName     "nomadwifi.exe"

[Setup]
AppId={{6E9A7D41-3C2B-4F58-9A6E-1D0B7C4E82A3}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}/docs
AppUpdatesURL={#AppURL}
VersionInfoVersion={#AppVersion}
VersionInfoCompany={#AppPublisher}
VersionInfoDescription={#AppName} Setup

DefaultDirName={localappdata}\Programs\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
DisableDirPage=auto
LicenseFile=..\LICENSE

; No elevation, ever. "lowest" also stops Inno offering a machine-wide install,
; which is deliberate: a Program Files install would need UAC for every update.
PrivilegesRequired=lowest

; The app is x64 only: the Go core is built without GOARCH so it follows the
; host, and CI runs on amd64. x64compatible still allows ARM64 emulation.
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

; Deliberately NOT using AppMutex, and not relying on Restart Manager either.
;
; AppMutex makes Setup ask the user to close the app, which under /SILENT with
; /SUPPRESSMSGBOXES -- i.e. how winget upgrades -- is an immediate abort. And
; Restart Manager cannot help, because the app cancels WM_CLOSE in order to
; hide to the tray, so a polite close request is ignored by design.
;
; PrepareToInstall below handles it instead: prompt when interactive, close
; without asking when silent. Nothing is lost either way -- settings live in
; HKCU and state.json, both written eagerly.
CloseApplications=no
RestartApplications=no

OutputDir=..\dist
OutputBaseFilename=NomadWiFi-Setup-{#AppVersion}
SetupIconFile=..\assets\nomadwifi.ico
UninstallDisplayIcon={app}\{#AppExeName}
UninstallDisplayName={#AppName}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked
Name: "startup";     Description: "Start {#AppName} when I sign in"; GroupDescription: "Startup:"
; The engine and the CLI are the same binary, so putting core\ on PATH is what
; makes `nomadwifi` work in a terminal. Never add {app} itself: the GUI shares
; the name nomadwifi.exe and would then be resolvable as its own engine.
Name: "addtopath";   Description: "Add the &nomadwifi command to my PATH"; GroupDescription: "Command line:"

[Files]
Source: "..\dist\NomadWiFi\nomadwifi.exe";      DestDir: "{app}";      Flags: ignoreversion
Source: "..\dist\NomadWiFi\core\nomadwifi.exe"; DestDir: "{app}\core"; Flags: ignoreversion
Source: "..\LICENSE";                            DestDir: "{app}";      DestName: "LICENSE.txt"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}";           Filename: "{app}\{#AppExeName}"
Name: "{group}\Uninstall {#AppName}"; Filename: "{uninstallexe}"
Name: "{userdesktop}\{#AppName}";     Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Registry]
; Startup is normally the app's own business (StartupManager writes this and
; self-heals a stale path), but seeding it here honours the wizard checkbox
; before the app has ever been launched. Same key, same value shape.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
    ValueType: string; ValueName: "NomadWiFi"; \
    ValueData: """{app}\{#AppExeName}"" --minimized"; \
    Flags: uninsdeletevalue; Tasks: startup

; Not created by Tasks, only removed: if the user unticked startup we must still
; clear an entry an earlier install left behind.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
    ValueType: none; ValueName: "NomadWiFi"; \
    Flags: deletevalue uninsdeletevalue; Tasks: not startup

[Run]
Filename: "{app}\{#AppExeName}"; Description: "Launch {#AppName}"; \
    Flags: nowait postinstall skipifsilent
; If Setup closed a running copy to replace its files, put it back the way it
; was found -- minimised to the tray, as it was before the upgrade.
Filename: "{app}\{#AppExeName}"; Parameters: "--minimized"; \
    Flags: nowait; Check: WasRunningBeforeInstall

[Code]
const
  { .NET Framework 4.8 == release 528040. The desktop app needs it and nothing
    in the build declares it, so check here rather than fail at launch. }
  Net48Release = 528040;
  DotNetUrl = 'https://dotnet.microsoft.com/download/dotnet-framework/net48';

function Net48Installed(): Boolean;
var
  Release: Cardinal;
begin
  Result := RegQueryDWordValue(HKLM,
    'SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full', 'Release', Release)
    and (Release >= Net48Release);
end;

function InitializeSetup(): Boolean;
var
  ErrorCode: Integer;
begin
  Result := True;
  if not Net48Installed() then
  begin
    if MsgBox('NomadWiFi needs the .NET Framework 4.8 runtime, which is not installed.'
      + #13#10#13#10 + 'It ships with Windows 10 version 1903 and later.'
      + #13#10#13#10 + 'Open the download page now?',
      mbError, MB_YESNO) = IDYES then
      ShellExec('open', DotNetUrl, '', '', SW_SHOWNORMAL, ewNoWait, ErrorCode);
    Result := False;
  end;
end;

// ---- closing a running copy before we replace its files ------------------

var
  ClosedForUpgrade: Boolean;

// Drives the conditional relaunch in [Run]: only true when Setup itself closed
// a running copy, so a fresh install does not silently start a tray process.
function WasRunningBeforeInstall(): Boolean;
begin
  Result := ClosedForUpgrade;
end;

function NomadWiFiIsRunning(): Boolean;
var
  ResultCode: Integer;
begin
  // tasklist filtered by image name; findstr decides whether anything matched,
  // because tasklist itself exits 0 even when it finds nothing.
  Result := Exec(ExpandConstant('{cmd}'),
    '/C tasklist /FI "IMAGENAME eq nomadwifi.exe" /NH | findstr /I "nomadwifi.exe"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode) and (ResultCode = 0);
end;

procedure StopNomadWiFi();
var
  ResultCode: Integer;
begin
  // One image name covers both the app and the engine it spawns, which is what
  // we want: a surviving engine would keep the old binary's files locked.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM nomadwifi.exe',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  ClosedForUpgrade := True;
  Sleep(1200);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  if not NomadWiFiIsRunning() then
    Exit;

  if WizardSilent() then
  begin
    // Unattended path, including winget upgrade. Closing is the only way the
    // update can proceed, and it is what the user asked for by upgrading.
    StopNomadWiFi();
    Exit;
  end;

  if MsgBox('NomadWiFi is running and has to close before it can be updated.'
    + #13#10#13#10 + 'Close it and continue?',
    mbConfirmation, MB_YESNO) = IDYES then
    StopNomadWiFi()
  else
    Result := 'NomadWiFi is still running. Close it from the system tray, then run Setup again.';
end;

// ---- PATH handling -------------------------------------------------------
// Inno has no built-in per-user PATH support, so read/modify/write HKCU. The
// entry added is the app's core subfolder, which holds the CLI.

function CorePathDir(): String;
begin
  Result := ExpandConstant('{app}') + '\core';
end;

procedure AddCoreToPath();
var
  Existing: String;
  Dir: String;
begin
  Dir := CorePathDir();
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', Existing) then
    Existing := '';

  { Already present: nothing to do. The delimiters guard against matching a
    longer path that merely starts with ours. }
  if Pos(Lowercase(';' + Dir + ';'), Lowercase(';' + Existing + ';')) > 0 then
    Exit;

  if (Existing <> '') and (Copy(Existing, Length(Existing), 1) <> ';') then
    Existing := Existing + ';';

  RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Existing + Dir);
end;

procedure RemoveCoreFromPath();
var
  Existing: String;
  Dir: String;
  P: Integer;
begin
  Dir := CorePathDir();
  if not RegQueryStringValue(HKCU, 'Environment', 'Path', Existing) then
    Exit;

  P := Pos(Lowercase(';' + Dir + ';'), Lowercase(';' + Existing + ';'));
  if P = 0 then
    Exit;

  { P indexes the synthetic ';'-wrapped copy, so it is already the 1-based
    offset of Dir within Existing. }
  Delete(Existing, P, Length(Dir) + 1);
  if (Existing <> '') and (Copy(Existing, Length(Existing), 1) = ';') then
    Delete(Existing, Length(Existing), 1);

  RegWriteExpandStringValue(HKCU, 'Environment', 'Path', Existing);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    if WizardIsTaskSelected('addtopath') then
      AddCoreToPath()
    else
      RemoveCoreFromPath();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  StateDir: String;
begin
  if CurUninstallStep <> usUninstall then
    Exit;

  RemoveCoreFromPath();

  // Learned state is the user's, not ours: offer, never assume. It holds the
  // Wi-Fi profiles NomadWiFi created plus which passwords turned out wrong.
  //
  // Never ask when silent. Under /SUPPRESSMSGBOXES a MsgBox returns its
  // default button, so prompting here would delete the user's data without
  // anyone having agreed to it. Keeping it is the recoverable choice.
  if UninstallSilent() then
    Exit;

  StateDir := ExpandConstant('{%USERPROFILE}') + '\.nomadwifi';
  if DirExists(StateDir) then
  begin
    if MsgBox('Also remove the networks and settings NomadWiFi learned?'
      + #13#10#13#10 + StateDir
      + #13#10#13#10 + 'Choose No to keep them for a future reinstall.',
      mbConfirmation, MB_YESNO) = IDNO then
      Exit;
    DelTree(StateDir, True, True, True);
  end;
end;
