using System;
using System.Diagnostics;
using Microsoft.Win32;

namespace NomadWiFi.UI.Services
{
    public static class StartupManager
    {
        private const string RunKey = @"Software\Microsoft\Windows\CurrentVersion\Run";
        private const string AppName = "NomadWiFi";

        public static bool IsStartupEnabled()
        {
            try
            {
                using (var key = Registry.CurrentUser.OpenSubKey(RunKey, false))
                {
                    if (key == null) return false;
                    var val = key.GetValue(AppName) as string;
                    return !string.IsNullOrEmpty(val);
                }
            }
            catch
            {
                return false;
            }
        }

        public static bool SetStartup(bool enable)
        {
            try
            {
                using (var key = Registry.CurrentUser.OpenSubKey(RunKey, true))
                {
                    if (key == null) return false;
                    if (enable)
                    {
                        key.SetValue(AppName, CurrentCommand());
                    }
                    else
                    {
                        key.DeleteValue(AppName, false);
                    }
                    return true;
                }
            }
            catch
            {
                return false;
            }
        }

        /// <summary>
        /// Rewrites an existing startup entry that points somewhere other than
        /// this executable.
        ///
        /// The entry records an absolute path, so moving or renaming the app
        /// leaves Windows launching a file that is no longer there while the
        /// checkbox still reads as enabled. Refreshing it on every start keeps
        /// the two in agreement instead of failing silently at next logon.
        /// </summary>
        public static void RefreshStartupPath()
        {
            try
            {
                using (var key = Registry.CurrentUser.OpenSubKey(RunKey, true))
                {
                    if (key == null) return;

                    var existing = key.GetValue(AppName) as string;
                    if (string.IsNullOrEmpty(existing)) return; // not enabled; nothing to correct

                    var expected = CurrentCommand();
                    if (!string.Equals(existing, expected, StringComparison.OrdinalIgnoreCase))
                    {
                        key.SetValue(AppName, expected);
                    }
                }
            }
            catch
            {
                // Startup registration is a convenience; never block launch on it.
            }
        }

        /// <summary>
        /// MainModule is the actual executable: the assembly location can
        /// differ once the app is shadow-copied.
        /// </summary>
        private static string CurrentCommand()
        {
            var exePath = Process.GetCurrentProcess().MainModule.FileName;
            return string.Format("\"{0}\" --minimized", exePath);
        }
    }
}
