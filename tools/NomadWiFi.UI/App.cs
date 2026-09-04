using System;
using System.Diagnostics;
using System.Linq;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows;

namespace NomadWiFi.UI
{
    public static class App
    {
        // Local\ scopes both handles to the logon session, which is the right
        // boundary for a per-user desktop app: two people using Fast User
        // Switching each get their own instance.
        private const string InstanceMutexName = @"Local\NomadWiFi.SingleInstance";
        private const string ActivateEventName = @"Local\NomadWiFi.Activate";

        [STAThread]
        public static void Main(string[] args)
        {
            // Take the instance lock before doing anything else. Holding it for
            // the lifetime of the process is what makes the check meaningful.
            bool isFirstInstance;
            using (var instanceLock = new Mutex(true, InstanceMutexName, out isFirstInstance))
            {
                if (!isFirstInstance)
                {
                    // Another copy owns the tray icon, the engine and the roam
                    // decisions. Starting a second one would fight it, so wake
                    // the existing window instead and leave quietly.
                    SignalRunningInstance();
                    return;
                }

                try
                {
                    Run(args);
                }
                catch (Exception ex)
                {
                    MessageBox.Show("NomadWiFi GUI Startup Error: " + ex.Message,
                        "NomadWiFi Error", MessageBoxButton.OK, MessageBoxImage.Error);
                }
                finally
                {
                    // Only the owner may release it; a second instance never
                    // acquired it and must not try.
                    try { instanceLock.ReleaseMutex(); }
                    catch (ApplicationException) { }
                }
            }
        }

        private static void Run(string[] args)
        {
            var app = new Application
            {
                ShutdownMode = ShutdownMode.OnExplicitShutdown
            };

            bool startMinimized = args != null &&
                args.Any(a => string.Equals(a, "--minimized", StringComparison.OrdinalIgnoreCase));

            var win = new MainWindow(startMinimized);
            if (!startMinimized)
            {
                win.Show();
            }

            ListenForActivation(app, win);
            app.Run();
        }

        /// <summary>
        /// Waits for a later launch to ask for the window, and shows it.
        /// The wait runs on its own background thread so it cannot hold up the
        /// UI, and marshals back through the dispatcher to touch the window.
        /// </summary>
        private static void ListenForActivation(Application app, MainWindow win)
        {
            EventWaitHandle handle;
            try
            {
                handle = new EventWaitHandle(false, EventResetMode.AutoReset, ActivateEventName);
            }
            catch
            {
                return; // without it a second launch simply exits; not fatal
            }

            var thread = new Thread(() =>
            {
                while (true)
                {
                    try
                    {
                        handle.WaitOne();
                        app.Dispatcher.BeginInvoke(new Action(win.PresentToUser));
                    }
                    catch
                    {
                        return; // the app is shutting down
                    }
                }
            });
            thread.IsBackground = true;
            thread.Start();
        }

        [DllImport("user32.dll")]
        private static extern bool AllowSetForegroundWindow(int processId);

        private static void SignalRunningInstance()
        {
            try
            {
                // Windows refuses SetForegroundWindow to a process that is not
                // already in front, so the running copy cannot raise itself.
                // This process was just launched by the user and therefore may:
                // hand that right over before asking it to show the window.
                GrantForegroundToRunningInstance();

                EventWaitHandle handle;
                if (EventWaitHandle.TryOpenExisting(ActivateEventName, out handle))
                {
                    using (handle) handle.Set();
                }
            }
            catch
            {
                // The running instance may be mid-shutdown. Exiting silently is
                // still better than starting a competing copy.
            }
        }

        private static void GrantForegroundToRunningInstance()
        {
            try
            {
                var self = Process.GetCurrentProcess();

                // The engine is also called nomadwifi, so matching on the
                // process name alone can hand foreground rights to the child
                // process instead of the window we want raised. Compare the
                // executable path: only another copy of *this* exe qualifies.
                string selfPath = null;
                try { selfPath = self.MainModule.FileName; } catch { }

                var siblings = Process.GetProcessesByName(self.ProcessName)
                    .Where(p => p.Id != self.Id)
                    .Where(p => selfPath == null || SameExecutable(p, selfPath))
                    .ToList();

                // Prefer one that already owns a window; a window-less match is
                // the tray-only case, where the handle appears once it unhides
                // and the grant below is what lets it come up.
                var running = siblings.FirstOrDefault(p => p.MainWindowHandle != IntPtr.Zero)
                              ?? siblings.FirstOrDefault();

                if (running != null) AllowSetForegroundWindow(running.Id);
            }
            catch
            {
                // Best effort: without the grant the window still un-hides, it
                // just may not come to the front.
            }
        }

        private static bool SameExecutable(Process candidate, string selfPath)
        {
            try
            {
                return string.Equals(candidate.MainModule.FileName, selfPath,
                    StringComparison.OrdinalIgnoreCase);
            }
            catch
            {
                // Access is denied for processes in other sessions; treating
                // those as non-matches is the safe default.
                return false;
            }
        }
    }
}
