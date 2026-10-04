package com.manga.reader

import android.app.Application
import android.content.Context
import android.util.Log

class MangaApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Log.i("MangaReader", "Application started")
    }
}