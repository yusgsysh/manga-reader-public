import { Component, type ReactNode } from "react";
import { Button } from "@cloudflare/kumo";
import { ArrowsClockwise, WarningCircle } from "@phosphor-icons/react";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  handleReload = () => {
    window.location.reload();
  };

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-kumo-base p-8 text-center">
          <span className="flex size-12 items-center justify-center rounded-2xl bg-kumo-danger/10 text-kumo-danger">
            <WarningCircle className="size-6" weight="fill" />
          </span>
          <h2 className="text-lg font-semibold">页面发生错误</h2>
          <p className="text-sm text-kumo-subtle">请尝试重新加载页面以恢复</p>
          <Button onClick={this.handleReload} variant="secondary" size="sm">
            <ArrowsClockwise className="mr-1 size-3.5" weight="bold" />
            重新加载
          </Button>
        </div>
      );
    }
    return this.props.children;
  }
}
