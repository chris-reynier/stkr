; STKR per-user installer for Windows (Inno Setup 6).
;
; Build with packaging/windows/build-setups.ps1: it writes
; build.generated.iss (AppVersion, VersionNumeric, Arch, RepoRoot, StkrExe,
; StkrwExe, OutDir, OutName as #define lines) and runs iscc on this file.
;
; The AppId below identifies STKR to Windows for upgrades and uninstall. Never
; change it (docs/contracts/release-assets.md).

#ifexist "build.generated.iss"
  #include "build.generated.iss"
#endif

#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef VersionNumeric
  #define VersionNumeric "0.0.0.0"
#endif
#ifndef Arch
  #define Arch "amd64"
#endif
#ifndef RepoRoot
  #define RepoRoot "..\.."
#endif
#ifndef StkrExe
  #define StkrExe RepoRoot + "\dist\stkr.exe"
#endif
#ifndef StkrwExe
  #define StkrwExe RepoRoot + "\dist\stkrw.exe"
#endif
#ifndef OutDir
  #define OutDir RepoRoot + "\dist"
#endif
#ifndef OutName
  #define OutName "stkr_" + AppVersion + "_windows_" + Arch + "_setup"
#endif

[Setup]
AppId={{6F3A9C2E-8B14-4D75-A1E0-3C5D7B92F4A8}
AppName=STKR
AppVersion={#AppVersion}
AppVerName=STKR {#AppVersion}
AppPublisher=STKR contributors
AppPublisherURL=https://github.com/chris-reynier/stkr
AppSupportURL=https://github.com/chris-reynier/stkr/issues
AppUpdatesURL=https://github.com/chris-reynier/stkr/releases
VersionInfoVersion={#VersionNumeric}
VersionInfoDescription=STKR setup
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\STKR
DisableProgramGroupPage=yes
ChangesEnvironment=yes
CloseApplications=yes
RestartApplications=no
UninstallDisplayName=STKR
UninstallDisplayIcon={app}\stkrw.exe
SetupIconFile={#RepoRoot}\packaging\windows\stkr.ico
OutputDir={#OutDir}
OutputBaseFilename={#OutName}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
#if Arch == "arm64"
ArchitecturesAllowed=arm64
ArchitecturesInstallIn64BitMode=arm64
#else
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
#endif

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "autostart"; Description: "Start STKR when I &sign in to Windows"
Name: "desktopicon"; Description: "Create a &desktop shortcut"; Flags: unchecked

[Files]
Source: "{#StkrExe}"; DestDir: "{app}"; DestName: "stkr.exe"; Flags: ignoreversion
Source: "{#StkrwExe}"; DestDir: "{app}"; DestName: "stkrw.exe"; Flags: ignoreversion
Source: "{#RepoRoot}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RepoRoot}\docs\user\*"; DestDir: "{app}\docs\user"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{autoprograms}\STKR"; Filename: "{app}\stkrw.exe"; AppUserModelID: "STKR.Desktop"
Name: "{autodesktop}\STKR"; Filename: "{app}\stkrw.exe"; AppUserModelID: "STKR.Desktop"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "STKR"; ValueData: """{app}\stkrw.exe"" --autostart"; Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\stkrw.exe"; Description: "Launch STKR"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: files; Name: "{app}\install.json"
Type: files; Name: "{app}\*.old"
Type: dirifempty; Name: "{app}"

[Code]
const
  EnvKey = 'Environment';
  RunKey = 'Software\Microsoft\Windows\CurrentVersion\Run';
  TrayClass = 'STKRDesktopTray';

// ---- PATH ------------------------------------------------------------------

function TrimSlash(const S: String): String;
begin
  Result := Trim(S);
  while (Length(Result) > 3) and (Result[Length(Result)] = '\') do
    Delete(Result, Length(Result), 1);
end;

function SameDir(const A, B: String): Boolean;
begin
  Result := CompareText(TrimSlash(A), TrimSlash(B)) = 0;
end;

// NextItem cuts the first ';' separated item off Rest.
function NextItem(var Rest: String): String;
var
  P: Integer;
begin
  P := Pos(';', Rest);
  if P = 0 then
  begin
    Result := Rest;
    Rest := '';
  end else
  begin
    Result := Copy(Rest, 1, P - 1);
    Rest := Copy(Rest, P + 1, Length(Rest));
  end;
end;

function PathContains(const Path, Dir: String): Boolean;
var
  Rest, Item: String;
begin
  Result := False;
  Rest := Path;
  while Rest <> '' do
  begin
    Item := NextItem(Rest);
    if (Item <> '') and SameDir(Item, Dir) then
    begin
      Result := True;
      Exit;
    end;
  end;
end;

// PathWithout returns Path without the entries equal to Dir; every other
// entry is kept exactly as it was.
function PathWithout(const Path, Dir: String): String;
var
  Rest, Item: String;
begin
  Result := '';
  Rest := Path;
  while Rest <> '' do
  begin
    Item := NextItem(Rest);
    if (Item <> '') and not SameDir(Item, Dir) then
    begin
      if Result <> '' then
        Result := Result + ';';
      Result := Result + Item;
    end;
  end;
end;

procedure AddToUserPath;
var
  Cur, Dir: String;
begin
  Dir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Cur) then
    Cur := '';
  if PathContains(Cur, Dir) then
    Exit;
  if (Cur <> '') and (Cur[Length(Cur)] <> ';') then
    Cur := Cur + ';';
  // Written as REG_EXPAND_SZ so %VARIABLES% already in the value keep working.
  RegWriteExpandStringValue(HKCU, EnvKey, 'Path', Cur + Dir);
end;

procedure RemoveFromUserPath;
var
  Cur, Dir, Next: String;
begin
  Dir := ExpandConstant('{app}');
  if not RegQueryStringValue(HKCU, EnvKey, 'Path', Cur) then
    Exit;
  if not PathContains(Cur, Dir) then
    Exit;
  Next := PathWithout(Cur, Dir);
  if Next = '' then
    RegDeleteValue(HKCU, EnvKey, 'Path')
  else
    RegWriteExpandStringValue(HKCU, EnvKey, 'Path', Next);
end;

// ---- running app -----------------------------------------------------------

// QuitRunningApp asks a running STKR tray app to quit and waits up to 5 s.
procedure QuitRunningApp(const Exe: String);
var
  Code, I: Integer;
begin
  if FindWindowByClassName(TrayClass) = 0 then
    Exit;
  if FileExists(Exe) then
    Exec(Exe, '--quit', '', SW_HIDE, ewWaitUntilTerminated, Code);
  for I := 1 to 25 do
  begin
    if FindWindowByClassName(TrayClass) = 0 then
      Exit;
    Sleep(200);
  end;
end;

// ---- install ---------------------------------------------------------------

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  QuitRunningApp(ExpandConstant('{app}\stkrw.exe'));
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    AddToUserPath;
    // Tells the updater which way this copy was installed.
    SaveStringToFile(ExpandConstant('{app}\install.json'),
      '{"method":"setup","version":"{#AppVersion}"}' + #13#10, False);
    // An upgrade must not keep a sign-in entry the user unticked.
    if not WizardIsTaskSelected('autostart') then
      RegDeleteValue(HKCU, RunKey, 'STKR');
  end;
end;

// ---- uninstall -------------------------------------------------------------

function DataDir: String;
begin
  Result := GetEnv('STKR_HOME');
  if Result = '' then
    Result := ExpandConstant('{%USERPROFILE}') + '\.stkr';
end;

function SafeToDelete(const Dir: String): Boolean;
var
  Profile: String;
begin
  Profile := ExpandConstant('{%USERPROFILE}');
  Result := (Length(Dir) > 8) and (not SameDir(Dir, Profile)) and DirExists(Dir);
end;

var
  DeleteData: Boolean;

function InitializeUninstall(): Boolean;
var
  Dir: String;
begin
  Result := True;
  DeleteData := False;
  QuitRunningApp(ExpandConstant('{app}\stkrw.exe'));
  // A silent uninstall always keeps the data.
  if UninstallSilent then
    Exit;
  Dir := DataDir;
  if not SafeToDelete(Dir) then
    Exit;
  if MsgBox('Also delete your STKR data (time log, ideas, bookmarks, settings and backups) in' + #13#10 + Dir + ' ?'
      + #13#10#13#10 + 'Choose No to keep it. You can then reinstall STKR later and carry on.',
      mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
    DeleteData := MsgBox('This deletes ' + Dir + ' and everything in it. It cannot be undone.' + #13#10#13#10
        + 'Delete it?', mbError, MB_YESNO or MB_DEFBUTTON2) = IDYES;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFromUserPath;
    RegDeleteValue(HKCU, RunKey, 'STKR');
    if DeleteData and SafeToDelete(DataDir) then
      DelTree(DataDir, True, True, True);
  end;
end;
