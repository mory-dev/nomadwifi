using System.Reflection;
using System.Resources;
using System.Runtime.InteropServices;
using System.Windows;

// Without these the shipped executable has blank file properties and reports
// itself as version 0.0.0.0, which looks like an unidentified binary next to
// the signed CLI. The Go core carries the same values via versioninfo.json.
[assembly: AssemblyTitle("NomadWiFi")]
[assembly: AssemblyDescription("Wi-Fi roaming and band optimizer for Windows")]
[assembly: AssemblyCompany("mory.dev")]
[assembly: AssemblyProduct("NomadWiFi")]
[assembly: AssemblyCopyright("MIT Licensed")]
[assembly: AssemblyVersion("1.2.0.0")]
[assembly: AssemblyFileVersion("1.2.0.0")]
[assembly: AssemblyInformationalVersion("1.2.0")]

[assembly: ComVisible(false)]
[assembly: NeutralResourcesLanguage("en")]

// Resource lookup stays assembly-local: the app ships no satellite assemblies
// and no theme-specific dictionaries.
[assembly: ThemeInfo(ResourceDictionaryLocation.None, ResourceDictionaryLocation.None)]
