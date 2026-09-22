using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Linq;
using System.Threading.Tasks;
using System.Windows.Forms;

namespace SplitTunnel.UI {
    internal sealed class InstallerForm : Surface {
        private readonly string installer;
        private readonly TextBox path;
        private readonly Label status;
        private readonly ProgressBar progress;
        private readonly Button install,browse,close;
        private bool working;
        internal InstallerForm(Dictionary<string,object> request):base("AnyConnect Split Tunnel 安装",new Size(604,351),Json.Text(request,"asset_root")) {
            installer=Json.Text(request,"installer_path");if(!File.Exists(installer))throw new FileNotFoundException("安装程序缺失。");
            FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=false;
            BackColor=Color.FromArgb(248,250,252);Label(this,26,24,540,34,"安装 AnyConnect Split Tunnel",16,true);
            Label(this,28,62,540,42,"选择安装位置。再次安装会覆盖旧版本；安装包已内置 OpenConnect、sing-box 和 IP 库。",9,false,Muted);
            Label(this,30,120,160,22,"安装位置",9);
            path=Place(this,new TextBox{Text=Json.Text(request,"default_install_dir")},30,146,430,28);
            browse=Button(this,474,144,104,32,"浏览...",delegate{using(var d=new FolderBrowserDialog{SelectedPath=path.Text,Description="选择安装位置"})if(d.ShowDialog(this)==DialogResult.OK)path.Text=d.SelectedPath;});
            progress=Place(this,new ProgressBar(),30,214,548,24);status=Label(this,30,250,548,44,"准备安装",9);
            install=Button(this,366,306,102,34,"开始安装");close=Button(this,476,306,102,34,"取消",delegate{Close();});
            install.Click+=async delegate {await Install();};FormClosing+=delegate(object sender,FormClosingEventArgs e){if(working)e.Cancel=true;};
        }
        internal static string Quote(string value) {return "\""+value.Replace("\"","\\\"").TrimEnd('\\')+"\"";}
        private int Run(string action,string directory=null){using(var p=new Process()){p.StartInfo=new ProcessStartInfo(installer,action+(directory==null?"":" "+Quote(directory))){UseShellExecute=false,CreateNoWindow=true};p.Start();p.WaitForExit();return p.ExitCode;}}
        private async Task Step(int percent,string text,string command,string directory){progress.Value=percent;status.Text=text;int code=await Task.Run(()=>Run(command,directory));if(code!=0)throw new InvalidOperationException(text+"失败（退出码 "+code+"）。请处理后重试。");}
        private async Task Install(){
            if(working)return;
            try{
                string dir=Path.GetFullPath(path.Text.Trim());if(dir.TrimEnd('\\')==Path.GetPathRoot(dir).TrimEnd('\\'))dir=Path.Combine(dir,"AnyConnectSplitTunnel");dir=dir.TrimEnd('\\');path.Text=dir;
                working=true;install.Enabled=browse.Enabled=path.Enabled=close.Enabled=false;
                await Step(5,"正在关闭旧版本并准备覆盖安装…","--prepare-overwrite",dir);
                await Step(10,"正在安装主程序及原生界面…","--install-payload",dir);
                status.Text="检查内置连接组件…";progress.Value=35;
                int bundled=await Task.Run(()=>Run("--has-bundled-tun-tools",dir));
                if(bundled!=0){int cisco=await Task.Run(()=>Run("--has-cisco"));if(cisco!=0){
                    string temp=Path.Combine(Path.GetTempPath(),"anyconnect-cisco-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(temp);
                    await Step(45,"正在释放 Cisco 官方安装包…","--extract-cisco",temp);
                    string package=Directory.GetFiles(temp).FirstOrDefault(f=>new[]{".msi",".exe"}.Contains(Path.GetExtension(f).ToLowerInvariant()));if(package==null)throw new FileNotFoundException("未找到 Cisco 安装文件。");
                    status.Text="请在 Cisco 官方安装窗口完成安装…";
                    int code=await Task.Run(delegate {var info=Path.GetExtension(package).Equals(".msi",StringComparison.OrdinalIgnoreCase)?new ProcessStartInfo("msiexec.exe","/i "+Quote(package)+" /norestart"):new ProcessStartInfo(package);info.UseShellExecute=true;using(var p=Process.Start(info)){p.WaitForExit();return p.ExitCode;}});
                    if(code!=0&&code!=3010)throw new InvalidOperationException("Cisco 安装未完成。");
                    if(await Task.Run(()=>Run("--has-cisco"))!=0)throw new InvalidOperationException("仍未检测到 Cisco 客户端，请按官方安装提示处理。");
                }}
                await Step(82,"正在创建桌面和开始菜单快捷方式…","--create-shortcuts",dir);
                await Step(88,"正在注册安装位置及标准卸载入口…","--write-install-state",dir);
                status.Text="正在启动程序…";progress.Value=94;
                Process.Start(new ProcessStartInfo(Path.Combine(dir,"anyconnect-split.exe")){WorkingDirectory=dir,UseShellExecute=true});
                progress.Value=100;status.Text="安装完成。首次连接请输入自己的账号密码。";
                MessageBox.Show(this,status.Text,"安装完成",MessageBoxButtons.OK,MessageBoxIcon.Information);
            }catch(Exception ex){status.Text="安装失败："+ex.Message;MessageBox.Show(this,ex.Message,"安装失败",MessageBoxButtons.OK,MessageBoxIcon.Error);}
            finally{working=false;install.Enabled=browse.Enabled=path.Enabled=close.Enabled=true;close.Text="完成";}
        }
    }
}
