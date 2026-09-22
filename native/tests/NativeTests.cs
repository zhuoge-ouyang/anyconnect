using System;
using System.Collections.Generic;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Text;
using System.Windows.Forms;

namespace SplitTunnel.UI {
    internal static class NativeTests {
        private static int checks;
        private static void Check(bool yes,string message){checks++;if(!yes)throw new Exception(message);}
        private static void Render(Form form,string path){using(var bitmap=new Bitmap(form.Width,form.Height)){form.DrawToBitmap(bitmap,new Rectangle(Point.Empty,form.Size));bitmap.Save(path);}}
        [STAThread] public static int Main(string[] args){
            try{
                Application.EnableVisualStyles();Application.SetCompatibleTextRenderingDefault(false);
                string root=args[0],evidence=args[1];Directory.CreateDirectory(evidence);
                var input=new Dictionary<string,object>{{"asset_root",root},{"snapshot_path",Path.Combine(evidence,"state.json")},{"command_dir",Path.Combine(evidence,"commands")}};
                var state=Json.Read("{\"status_text\":\"状态：TUN 分流已启用\",\"current_site\":\"测试线路\",\"split_mode\":\"domestic_direct\",\"split_tunnel_enabled\":true,\"backend\":\"openconnect_tun\",\"foreign_domains\":[\"example.com\"],\"foreign_cidrs\":[\"203.0.113.8/32\"],\"smart_state\":\"\"}");
                File.WriteAllText((string)input["snapshot_path"],Json.Codec.Serialize(state));
                using(var f=new DashboardForm(input)){
                    f.Show();Application.DoEvents();f.Apply(state);
                    Check(f.Status.Text=="已连接"&&f.Disconnect.Visible&&!f.Reconnect.Visible,"connected state");
                    Check(f.Domestic.Checked&&!f.VPN.Checked,"domestic mode");
                    Check(!Directory.Exists((string)input["command_dir"]),"hydration emitted a command");
                    f.QuickInput.Text="https://openapi.longbridge.com/path?q=1";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("openapi.longbridge.com"),"URL add");
                    f.QuickInput.Text="example.com";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("已在"),"duplicate domain");
                    f.QuickInput.Text="203.0.113.9";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("/32"),"IPv4 add");
                    f.QuickInput.Text="https://example.org:8443/path";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("example.org"),"URL port");
                    f.QuickInput.Text="2001:db8::1234/64";f.QuickAdd.PerformClick();Check(f.QuickHint.Text.Contains("2001:db8::/64"),"IPv6 normalization");
                    int before=Directory.GetFiles((string)input["command_dir"],"*.json").Length;
                    f.QuickInput.Text="not a URL";f.QuickAdd.PerformClick();Check(Directory.GetFiles((string)input["command_dir"],"*.json").Length==before,"invalid value emitted");
                    f.VPN.Checked=true;Check(f.VPN.Checked,"mode toggle");f.Domestic.Checked=true;
                    f.QuickInput.Clear();f.QuickHint.Text="";f.Apply(state);Render(f,Path.Combine(evidence,"dashboard.png"));
                    f.Page(true);Application.DoEvents();Check(f.SettingsPage.Visible&&!f.ConnectPage.Visible,"settings navigation");
                    f.AutoStart.Checked=true;f.UpdateIPDB.PerformClick();f.Send("view_log");
                    Render(f,Path.Combine(evidence,"settings.png"));
                    f.Page(false);state["smart_state"]="running";f.Apply(state);Check(!f.QuickAdd.Enabled&&f.Smart.Text=="取消智能选线","smart selection interlock");
                    state["smart_state"]="";state["status_text"]="状态：VPN 未连接";f.Apply(state);Check(f.Reconnect.Visible&&!f.Disconnect.Visible,"disconnected state");
                    state["status_text"]="状态：正在连接";f.Apply(state);Check(!f.Reconnect.Enabled&&f.Status.Text=="连接处理中","busy state");
                    File.WriteAllText((string)input["snapshot_path"],"invalid");f.RefreshState();Check(f.Latest==null&&!f.QuickAdd.Enabled&&f.Status.Text=="状态不可用","invalid snapshot guard");
                    File.WriteAllText((string)input["snapshot_path"],Json.Codec.Serialize(state));f.RefreshState();Check(f.Latest!=null,"snapshot recovery");
                    f.ClientSize=new Size(900,740);Application.DoEvents();Check(f.ConnectPage.Right<=f.ConnectPage.Parent.ClientSize.Width,"minimum layout");
                    f.Close();
                }
                foreach(var pair in new[]{new[]{"203.0.113.42/24","203.0.113.0/24"},new[]{"https://[2001:db8::1]/p","2001:db8::1/128"},new[]{"EXAMPLE.COM/path","example.com"}})Check(Target.Parse(pair[0]).Value==pair[1],"normalization "+pair[0]);
                foreach(var bad in new[]{"203.0.113.8/33","2001:db8::/129","ftp://example.com","https://user:pass@example.com","invalid","a b.com"}){bool rejected=false;try{Target.Parse(bad);}catch(FormatException){rejected=true;}Check(rejected,"accepted bad target");}
                var writer=new CommandWriter((string)input["command_dir"]);
                using(var f=new WhitelistForm(root,writer,state)){
                    f.Show();Application.DoEvents();f.Input.Text="https://api.example.com/path";f.AddDomain.PerformClick();Check(f.Items.Items.Contains("域名 | api.example.com"),"whitelist domain");
                    f.Input.Text="203.0.113.99/24";f.AddIP.PerformClick();Check(f.Items.Items.Contains("IP/CIDR | 203.0.113.0/24"),"whitelist CIDR");
                    f.Items.SelectedIndex=f.Items.Items.Count-1;f.Remove.PerformClick();Check(!f.Items.Items.Contains("IP/CIDR | 203.0.113.0/24"),"whitelist remove");
                    Render(f,Path.Combine(evidence,"whitelist.png"));f.Close();
                }
                input["sites"]=new object[]{new Dictionary<string,object>{{"Name","深圳 | 测试;节点"},{"Server","https://example.invalid:443"}},new Dictionary<string,object>{{"Name","日本"},{"Server","https://jp.example.invalid"}}};
                input["preferred_site"]="深圳 | 测试;节点";input["saved_username"]="测试用户";input["remember"]=true;
                using(var f=new LoginForm(input)){
                    f.Show();Application.DoEvents();Check(Convert.ToString(f.Sites.SelectedItem)=="深圳 | 测试;节点","site names must not use delimiters");
                    f.Sites.DroppedDown=true;Application.DoEvents();f.Sites.DroppedDown=false;Application.DoEvents();Check(f.Sites.DroppedDown,"dropdown closes without committed selection");
                    typeof(ComboBox).GetMethod("OnSelectionChangeCommitted",System.Reflection.BindingFlags.Instance|System.Reflection.BindingFlags.NonPublic).Invoke(f.Sites,new object[]{EventArgs.Empty});
                    f.Sites.DroppedDown=false;Application.DoEvents();Check(!f.Sites.DroppedDown,"dropdown reopens after committed selection");
                    Check(f.Password.UseSystemPasswordChar&&!f.Valid(),"password masking and blank validation");
                    f.Password.Text="test-only-secret";Check(f.Valid(),"valid login");Render(f,Path.Combine(evidence,"login.png"));
                    f.Connect.PerformClick();Check(Json.Flag(f.Result,"ok")&&Json.Text(f.Result,"server")=="https://example.invalid:443"&&Json.Text(f.Result,"password")=="test-only-secret","login response");
                }
                using(var f=new LoginForm(input)){f.Show();Application.DoEvents();f.Close();Check(!Json.Flag(f.Result,"ok"),"cancelled login");}
                using(var f=new ContactForm(root)){f.Show();Application.DoEvents();Render(f,Path.Combine(evidence,"contact.png"));f.Close();}
                using(var f=new RechargeForm(root)){f.Show();Application.DoEvents();Check(f.Controls.OfType<TextBox>().Single().Text.Contains("【六、安装其他设备】"),"complete recharge text");f.Close();}
                state["smart_candidate"]="测试推荐线路";state["smart_attempts"]=3;state["smart_successes"]=3;
                using(var f=new RecommendationForm(root,writer,state)){f.Show();Application.DoEvents();Check(!f.ControlBox,"recommendation close bypass");f.Controls.OfType<Button>().Single(b=>b.Text=="立即采用").PerformClick();}
                using(var f=new RecommendationForm(root,writer,state)){f.Show();Application.DoEvents();f.Controls.OfType<Button>().Single(b=>b.Text=="恢复原线路").PerformClick();}
                var setup=new Dictionary<string,object>{{"asset_root",root},{"installer_path",System.Diagnostics.Process.GetCurrentProcess().MainModule.FileName},{"default_install_dir",@"C:\Program Files\AnyConnectSplitTunnel"}};
                using(var f=new InstallerForm(setup)){f.Show();Application.DoEvents();Render(f,Path.Combine(evidence,"installer.png"));f.Close();}
                Check(!Directory.GetFiles((string)input["command_dir"],"*.tmp").Any(),"partial command file remained");
                var commands=Directory.GetFiles((string)input["command_dir"],"*.json").Select(p=>Json.Read(File.ReadAllText(p))).ToArray();
                foreach(var action in new[]{"add_foreign_domain","add_foreign_cidr","remove_foreign_cidr","set_split_mode","toggle_autostart","update_ipdb","view_log","smart_select_accept","smart_select_restore"})Check(commands.Any(c=>Json.Text(c,"action")==action),"missing action "+action);
                Console.WriteLine("PASS: "+checks+" native WinForms checks");return 0;
            }catch(Exception ex){Console.Error.WriteLine(ex);return 1;}
        }
    }
}
