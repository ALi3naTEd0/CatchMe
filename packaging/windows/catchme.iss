; Inno Setup script for the CatchMe installer. Built by build.ps1 with
; /DAppVersion and /DSourceDir (the Flutter release folder with catchmed.exe).

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{8A3F14DA-108A-4B88-A3B8-2322E38EC182}
AppName=CatchMe
AppVersion={#AppVersion}
AppPublisher=ALi3naTEd0
AppPublisherURL=https://github.com/ALi3naTEd0/CatchMe
DefaultDirName={autopf}\CatchMe
DefaultGroupName=CatchMe
DisableProgramGroupPage=yes
; Installs for the current user without asking for admin rights.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputBaseFilename=CatchMe-{#AppVersion}-windows-x64-setup
UninstallDisplayIcon={app}\catchme.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
; Close a running CatchMe (and its engine) before upgrading.
CloseApplications=yes

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: recursesubdirs ignoreversion

[Icons]
Name: "{group}\CatchMe"; Filename: "{app}\catchme.exe"
Name: "{autodesktop}\CatchMe"; Filename: "{app}\catchme.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\catchme.exe"; Description: "Open CatchMe"; Flags: nowait postinstall skipifsilent
