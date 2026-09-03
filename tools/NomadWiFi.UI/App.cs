using System;
using System.Windows;

namespace NomadWiFi.UI
{
    public static class App
    {
        [STAThread]
        public static void Main()
        {
            try
            {
                var app = new Application
                {
                    ShutdownMode = ShutdownMode.OnExplicitShutdown
                };
                var win = new MainWindow();
                app.Run(win);
            }
            catch (Exception ex)
            {
                MessageBox.Show("NomadWiFi GUI Startup Error: " + ex.Message, "NomadWiFi Error", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }
    }
}
