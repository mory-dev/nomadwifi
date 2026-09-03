using System;
using System.Linq;
using System.Windows;

namespace NomadWiFi.UI
{
    public static class App
    {
        [STAThread]
        public static void Main(string[] args)
        {
            try
            {
                var app = new Application
                {
                    ShutdownMode = ShutdownMode.OnExplicitShutdown
                };
                bool startMinimized = args != null && args.Any(a => string.Equals(a, "--minimized", StringComparison.OrdinalIgnoreCase));
                var win = new MainWindow(startMinimized);
                if (!startMinimized)
                {
                    win.Show();
                }
                app.Run();
            }
            catch (Exception ex)
            {
                MessageBox.Show("NomadWiFi GUI Startup Error: " + ex.Message, "NomadWiFi Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }
    }
}
